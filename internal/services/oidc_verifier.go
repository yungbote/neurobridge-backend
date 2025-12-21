package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ExternalIdentity struct {
	Provider      string
	Sub           string
	Email         string
	EmailVerified bool
	FirstName     string
	LastName      string
	NonceClaim    string // raw value from token
}

type OIDCVerifier interface {
	VerifyGoogleIDToken(ctx context.Context, idToken string, expectedNonceHash string) (*ExternalIdentity, error)
	VerifyAppleIDToken(ctx context.Context, idToken string, expectedNonceHash string) (*ExternalIdentity, error)
}

type oidcVerifier struct {
	httpClient     *http.Client
	googleClientID string
	appleClientID  string
	google         *providerVerifier
	apple          *providerVerifier
}

func NewOIDCVerifier(httpClient *http.Client, googleClientID, appleClientID string) (OIDCVerifier, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if strings.TrimSpace(googleClientID) == "" {
		return nil, fmt.Errorf("GOOGLE_OIDC_CLIENT_ID is required")
	}
	if strings.TrimSpace(appleClientID) == "" {
		return nil, fmt.Errorf("APPLE_OIDC_CLIENT_ID is required")
	}

	googleIss := []string{"accounts.google.com", "https://accounts.google.com"}
	g := newProviderVerifier(
		httpClient,
		"https://accounts.google.com/.well-known/openid-configuration",
		googleIss,
		googleClientID,
		[]string{"RS256"},
	)

	a := newProviderVerifier(
		httpClient,
		"https://appleid.apple.com/.well-known/openid-configuration",
		[]string{"https://appleid.apple.com"},
		appleClientID,
		[]string{"ES256"},
	)

	return &oidcVerifier{
		httpClient:     httpClient,
		googleClientID: googleClientID,
		appleClientID:  appleClientID,
		google:         g,
		apple:          a,
	}, nil
}

func (v *oidcVerifier) VerifyGoogleIDToken(ctx context.Context, idToken string, expectedNonceHash string) (*ExternalIdentity, error) {
	claims, err := v.google.verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	out := claimsToExternal("google", claims)
	if err := verifyNonceAgainstHash("google", out.NonceClaim, expectedNonceHash); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *oidcVerifier) VerifyAppleIDToken(ctx context.Context, idToken string, expectedNonceHash string) (*ExternalIdentity, error) {
	claims, err := v.apple.verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	out := claimsToExternal("apple", claims)
	if err := verifyNonceAgainstHash("apple", out.NonceClaim, expectedNonceHash); err != nil {
		return nil, err
	}
	return out, nil
}

func verifyNonceAgainstHash(provider, nonceClaim, expectedNonceHash string) error {
	if strings.TrimSpace(expectedNonceHash) == "" {
		return fmt.Errorf("missing expected nonce hash")
	}
	if strings.TrimSpace(nonceClaim) == "" {
		return fmt.Errorf("missing nonce claim in id_token")
	}

	// - Google: nonce claim is typically the raw nonce -> hash it and compare.
	// - Apple: nonce claim is often already the hashed value depending on client; accept either direct match or hash(claim).
	if constantTimeEq(nonceClaim, expectedNonceHash) {
		return nil
	}
	if constantTimeEq(hashNonceBase64URL(nonceClaim), expectedNonceHash) {
		return nil
	}
	return fmt.Errorf("nonce mismatch for provider=%s", provider)
}

func hashNonceBase64URL(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ----- internals -----

type oidcDiscovery struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

type providerVerifier struct {
	httpClient   *http.Client
	discoveryURL string
	allowedIss   []string
	requiredAud  string
	algAllow     []string

	jwks          *jwksCache
	discoveryOnce sync.Once
	discoveryErr  error
}

func newProviderVerifier(httpClient *http.Client, discoveryURL string, allowedIss []string, requiredAud string, algAllow []string) *providerVerifier {
	return &providerVerifier{
		httpClient:   httpClient,
		discoveryURL: discoveryURL,
		allowedIss:   allowedIss,
		requiredAud:  requiredAud,
		algAllow:     algAllow,
		jwks:         newJWKSCache(httpClient),
	}
}

func (p *providerVerifier) ensureDiscovery(ctx context.Context) error {
	p.discoveryOnce.Do(func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.discoveryURL, nil)
		res, err := p.httpClient.Do(req)
		if err != nil {
			p.discoveryErr = err
			return
		}
		defer res.Body.Close()

		if res.StatusCode < 200 || res.StatusCode >= 300 {
			p.discoveryErr = fmt.Errorf("discovery request failed: %s", res.Status)
			return
		}

		var d oidcDiscovery
		if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
			p.discoveryErr = err
			return
		}
		if strings.TrimSpace(d.JWKSURI) == "" {
			p.discoveryErr = fmt.Errorf("discovery missing jwks_uri")
			return
		}
		p.jwks.setURL(d.JWKSURI)
	})
	return p.discoveryErr
}

func (p *providerVerifier) verify(ctx context.Context, tokenString string) (jwt.MapClaims, error) {
	if strings.TrimSpace(tokenString) == "" {
		return nil, fmt.Errorf("id_token is empty")
	}
	if err := p.ensureDiscovery(ctx); err != nil {
		return nil, fmt.Errorf("oidc discovery error: %w", err)
	}

	parser := jwt.NewParser(jwt.WithValidMethods(p.algAllow))
	claims := jwt.MapClaims{}

	tok, err := parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if strings.TrimSpace(kid) == "" {
			return nil, fmt.Errorf("missing kid")
		}
		pub, err := p.jwks.getKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return pub, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid id_token: %w", err)
	}
	if tok == nil || !tok.Valid {
		return nil, fmt.Errorf("invalid id_token")
	}

	// Time-based validation (jwt/v5 MapClaims does not expose Valid()).
	if err := validateTimeClaims(claims, time.Now(), 0); err != nil {
		return nil, err
	}

	iss, _ := claims["iss"].(string)
	if !containsIssuer(p.allowedIss, iss) {
		return nil, fmt.Errorf("issuer mismatch: %q", iss)
	}

	if !audContains(claims["aud"], p.requiredAud) {
		return nil, fmt.Errorf("audience mismatch")
	}

	sub, _ := claims["sub"].(string)
	if strings.TrimSpace(sub) == "" {
		return nil, fmt.Errorf("missing sub")
	}

	return claims, nil
}

func validateTimeClaims(claims jwt.MapClaims, now time.Time, leeway time.Duration) error {
	// exp is required for ID tokens
	expAny, ok := claims["exp"]
	if !ok {
		return fmt.Errorf("missing exp")
	}
	exp, err := parseNumericTime(expAny)
	if err != nil {
		return fmt.Errorf("invalid exp: %w", err)
	}
	if now.After(exp.Add(leeway)) {
		return fmt.Errorf("token expired")
	}

	// nbf is optional
	if nbfAny, ok := claims["nbf"]; ok {
		nbf, err := parseNumericTime(nbfAny)
		if err != nil {
			return fmt.Errorf("invalid nbf: %w", err)
		}
		if now.Add(leeway).Before(nbf) {
			return fmt.Errorf("token not valid yet")
		}
	}

	// iat is optional; reject tokens issued too far in the future
	if iatAny, ok := claims["iat"]; ok {
		iat, err := parseNumericTime(iatAny)
		if err != nil {
			return fmt.Errorf("invalid iat: %w", err)
		}
		if iat.After(now.Add(5 * time.Minute)) {
			return fmt.Errorf("token issued in the future")
		}
	}

	return nil
}

func parseNumericTime(v any) (time.Time, error) {
	// JWT numeric dates are seconds since epoch
	var sec int64

	switch x := v.(type) {
	case float64:
		sec = int64(x)
	case float32:
		sec = int64(x)
	case int64:
		sec = x
	case int:
		sec = int64(x)
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return time.Time{}, err
		}
		sec = n
	case string:
		// sometimes providers serialize to string
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		sec = n
	default:
		return time.Time{}, fmt.Errorf("unexpected type %T", v)
	}

	if sec <= 0 {
		return time.Time{}, fmt.Errorf("non-positive numeric date")
	}
	return time.Unix(sec, 0).UTC(), nil
}

func containsIssuer(list []string, iss string) bool {
	for _, v := range list {
		if constantTimeEq(v, iss) {
			return true
		}
	}
	return false
}

func audContains(aud any, required string) bool {
	switch v := aud.(type) {
	case string:
		return v == required
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && s == required {
				return true
			}
		}
	}
	return false
}

func claimsToExternal(provider string, c jwt.MapClaims) *ExternalIdentity {
	out := &ExternalIdentity{Provider: provider}

	if s, _ := c["sub"].(string); s != "" {
		out.Sub = s
	}
	if e, _ := c["email"].(string); e != "" {
		out.Email = e
	}
	out.EmailVerified = parseBool(c["email_verified"])

	if gn, _ := c["given_name"].(string); gn != "" {
		out.FirstName = gn
	}
	if fn, _ := c["family_name"].(string); fn != "" {
		out.LastName = fn
	}
	if n, _ := c["nonce"].(string); n != "" {
		out.NonceClaim = n
	}

	return out
}

func parseBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true") || x == "1"
	case float64:
		return x != 0
	default:
		return false
	}
}

// ----- JWKS cache (supports RSA + EC) -----

type jwksCache struct {
	httpClient *http.Client

	mu      sync.RWMutex
	jwksURL string
	keys    map[string]any // kid -> *rsa.PublicKey or *ecdsa.PublicKey

	fetchedAt time.Time
	ttl       time.Duration
}

func newJWKSCache(httpClient *http.Client) *jwksCache {
	return &jwksCache{
		httpClient: httpClient,
		keys:       map[string]any{},
		ttl:        6 * time.Hour,
	}
}

func (j *jwksCache) setURL(url string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jwksURL = url
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`

	// RSA
	N string `json:"n"`
	E string `json:"e"`

	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (j *jwksCache) getKey(ctx context.Context, kid string) (any, error) {
	j.mu.RLock()
	key := j.keys[kid]
	stale := time.Since(j.fetchedAt) > j.ttl
	url := j.jwksURL
	j.mu.RUnlock()

	if key != nil && !stale {
		return key, nil
	}
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("jwks url not set")
	}

	if err := j.refresh(ctx, url); err != nil {
		// fallback to cached key if present
		j.mu.RLock()
		key = j.keys[kid]
		j.mu.RUnlock()
		if key != nil {
			return key, nil
		}
		return nil, err
	}

	j.mu.RLock()
	defer j.mu.RUnlock()
	key = j.keys[kid]
	if key == nil {
		return nil, fmt.Errorf("kid not found in jwks: %s", kid)
	}
	return key, nil
}

func (j *jwksCache) refresh(ctx context.Context, url string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := j.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("jwks fetch failed: %s", res.Status)
	}

	var set jwkSet
	if err := json.NewDecoder(res.Body).Decode(&set); err != nil {
		return err
	}

	next := map[string]any{}
	for _, k := range set.Keys {
		if strings.TrimSpace(k.Kid) == "" {
			continue
		}
		switch k.Kty {
		case "RSA":
			pub, err := rsaFromModExp(k.N, k.E)
			if err == nil {
				next[k.Kid] = pub
			}
		case "EC":
			pub, err := ecdsaFromXY(k.Crv, k.X, k.Y)
			if err == nil {
				next[k.Kid] = pub
			}
		}
	}

	if len(next) == 0 {
		return fmt.Errorf("jwks contained no usable keys")
	}

	j.mu.Lock()
	j.keys = next
	j.fetchedAt = time.Now()
	j.mu.Unlock()
	return nil
}

func rsaFromModExp(nB64, eB64 string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nb)
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func ecdsaFromXY(crv, xB64, yB64 string) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch crv {
	case "P-256":
		curve = elliptic.P256()
	default:
		return nil, fmt.Errorf("unsupported curve: %s", crv)
	}

	xb, err := base64.RawURLEncoding.DecodeString(xB64)
	if err != nil {
		return nil, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(yB64)
	if err != nil {
		return nil, err
	}

	x := new(big.Int).SetBytes(xb)
	y := new(big.Int).SetBytes(yb)

	if !curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("invalid EC point")
	}

	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}










