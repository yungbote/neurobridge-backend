package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type UserService interface {
	GetMe(dbc dbctx.Context) (*types.User, error)
	GetPersonalizationPrefs(dbc dbctx.Context) (*types.UserPersonalizationPrefs, error)

	// NEW
	UpdatePreferredTheme(ctx context.Context, preferredTheme string) (*types.User, error)
	UpdateThemePreferences(ctx context.Context, preferredTheme *string, preferredUITheme *string) (*types.User, error)
	UpdateName(ctx context.Context, firstName, lastName string) (*types.User, error)
	UpdateAvatarColor(ctx context.Context, avatarColor string) (*types.User, error)
	UploadAvatarImage(ctx context.Context, raw []byte) (*types.User, error)
	UpsertPersonalizationPrefs(ctx context.Context, prefsJSON []byte) (*types.UserPersonalizationPrefs, error)
}

type userService struct {
	db            *gorm.DB
	log           *logger.Logger
	userRepo      repos.UserRepo
	prefsRepo     repos.UserPersonalizationPrefsRepo
	avatarService AvatarService
}

var validThemePreferences = map[string]struct{}{
	"light":  {},
	"dark":   {},
	"system": {},
}

var validUIThemes = map[string]struct{}{
	"classic": {},
	"slate":   {},
	"dune":    {},
	"sage":    {},
	"aurora":  {},
	"ink":     {},
	"linen":   {},
	"ember":   {},
	"harbor":  {},
	"moss":    {},
}

func normalizeThemeInput(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NewUserService(db *gorm.DB, log *logger.Logger, userRepo repos.UserRepo, prefsRepo repos.UserPersonalizationPrefsRepo, avatarService AvatarService) UserService {
	serviceLog := log.With("service", "UserService")
	return &userService{
		db:            db,
		log:           serviceLog,
		userRepo:      userRepo,
		prefsRepo:     prefsRepo,
		avatarService: avatarService,
	}
}

func (us *userService) GetMe(dbc dbctx.Context) (*types.User, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil {
		us.log.Warn("Request data not set in context")
		return nil, fmt.Errorf("request data not set in context")
	}
	if rd.UserID == uuid.Nil {
		us.log.Warn("User id not set in request data")
		return nil, fmt.Errorf("user id not set in request data")
	}

	getUser := func(dbc dbctx.Context, userID uuid.UUID) (*types.User, error) {
		found, err := us.userRepo.GetByIDs(dbc, []uuid.UUID{userID})
		if err != nil {
			return nil, fmt.Errorf("error fetching user: %w", err)
		}
		if len(found) == 0 || found[0] == nil {
			return nil, fmt.Errorf("user does not exist")
		}
		return found[0], nil
	}

	if dbc.Tx != nil {
		return getUser(dbc, rd.UserID)
	}

	var theUser *types.User
	if err := us.db.WithContext(dbc.Ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: dbc.Ctx, Tx: tx}
		u, err := getUser(inner, rd.UserID)
		if err != nil {
			return err
		}
		theUser = u
		return nil
	}); err != nil {
		us.log.Warn("GetMe transaction error:", "error", err)
		return nil, err
	}
	return theUser, nil
}

func (us *userService) GetPersonalizationPrefs(dbc dbctx.Context) (*types.UserPersonalizationPrefs, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}
	if us.prefsRepo == nil {
		return nil, fmt.Errorf("prefs repo not configured")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = us.db
	}
	return us.prefsRepo.GetByUserID(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, rd.UserID)
}

func (us *userService) UpdatePreferredTheme(ctx context.Context, preferredTheme string) (*types.User, error) {
	return us.UpdateThemePreferences(ctx, &preferredTheme, nil)
}

func (us *userService) UpdateThemePreferences(ctx context.Context, preferredTheme *string, preferredUITheme *string) (*types.User, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}

	if preferredTheme == nil && preferredUITheme == nil {
		return nil, fmt.Errorf("no theme updates provided")
	}

	var normalizedTheme *string
	if preferredTheme != nil {
		normalized := normalizeThemeInput(*preferredTheme)
		if _, ok := validThemePreferences[normalized]; !ok {
			return nil, fmt.Errorf("invalid preferred_theme")
		}
		normalizedTheme = &normalized
	}

	var normalizedUITheme *string
	if preferredUITheme != nil {
		normalized := normalizeThemeInput(*preferredUITheme)
		if _, ok := validUIThemes[normalized]; !ok {
			return nil, fmt.Errorf("invalid preferred_ui_theme")
		}
		normalizedUITheme = &normalized
	}

	var out *types.User
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if normalizedTheme != nil {
			if err := us.userRepo.UpdatePreferredTheme(dbc, rd.UserID, *normalizedTheme); err != nil {
				return err
			}
		}
		if normalizedUITheme != nil {
			if err := us.userRepo.UpdatePreferredUITheme(dbc, rd.UserID, *normalizedUITheme); err != nil {
				return err
			}
		}
		u, err := us.userRepo.GetByIDs(dbc, []uuid.UUID{rd.UserID})
		if err != nil || len(u) == 0 {
			return fmt.Errorf("failed to reload user")
		}
		out = u[0]
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (us *userService) UpdateName(ctx context.Context, firstName, lastName string) (*types.User, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}

	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)
	if firstName == "" || lastName == "" {
		return nil, fmt.Errorf("first_name and last_name required")
	}

	var out *types.User
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		// Load user so we preserve color
		found, err := us.userRepo.GetByIDs(dbc, []uuid.UUID{rd.UserID})
		if err != nil || len(found) == 0 || found[0] == nil {
			return fmt.Errorf("user not found")
		}
		u := found[0]

		// Update name
		if err := us.userRepo.UpdateName(dbc, rd.UserID, firstName, lastName); err != nil {
			return err
		}

		// Update struct so avatar generator uses new initials but same AvatarColor
		u.FirstName = firstName
		u.LastName = lastName

		// Regenerate initials avatar (keeps existing AvatarColor)
		if err := us.avatarService.CreateAndUploadUserAvatar(dbc, u); err != nil {
			return err
		}

		// Persist avatar fields
		if err := us.userRepo.UpdateAvatarFields(dbc, rd.UserID, u.AvatarBucketKey, u.AvatarURL); err != nil {
			return err
		}

		out = u
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (us *userService) UpdateAvatarColor(ctx context.Context, avatarColor string) (*types.User, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}

	avatarColor = strings.ToUpper(strings.TrimSpace(avatarColor))
	// validation: AvatarService will normalize/validate; but we should reject empty
	if avatarColor == "" {
		return nil, fmt.Errorf("avatar_color required")
	}

	var out *types.User
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		found, err := us.userRepo.GetByIDs(dbc, []uuid.UUID{rd.UserID})
		if err != nil || len(found) == 0 || found[0] == nil {
			return fmt.Errorf("user not found")
		}
		u := found[0]

		// Update avatar_color in DB first
		if err := us.userRepo.UpdateAvatarColor(dbc, rd.UserID, avatarColor); err != nil {
			return err
		}
		u.AvatarColor = avatarColor

		// Regenerate initials avatar with new color
		if err := us.avatarService.CreateAndUploadUserAvatar(dbc, u); err != nil {
			return err
		}
		if err := us.userRepo.UpdateAvatarFields(dbc, rd.UserID, u.AvatarBucketKey, u.AvatarURL); err != nil {
			return err
		}

		out = u
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (us *userService) UploadAvatarImage(ctx context.Context, raw []byte) (*types.User, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty upload")
	}

	var out *types.User
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		found, err := us.userRepo.GetByIDs(dbc, []uuid.UUID{rd.UserID})
		if err != nil || len(found) == 0 || found[0] == nil {
			return fmt.Errorf("user not found")
		}
		u := found[0]

		// Upload processed image (512 circle)
		if err := us.avatarService.CreateAndUploadUserAvatarFromImage(dbc, u, raw); err != nil {
			return err
		}

		if err := us.userRepo.UpdateAvatarFields(dbc, rd.UserID, u.AvatarBucketKey, u.AvatarURL); err != nil {
			return err
		}

		out = u
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Personalization prefs ---

func sliceString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func anyBool(v any, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

func anyInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case float32:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		if i64, err := t.Int64(); err == nil {
			return int(i64), true
		}
	}
	if v == nil {
		return 0, false
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, false
		}
		if i64, err := json.Number(s).Int64(); err == nil {
			return int(i64), true
		}
	}
	return 0, false
}

func anyStringArray(v any) []string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			s = strings.ToLower(strings.TrimSpace(s))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

var allowedPersonalizationLanguages = map[string]struct{}{
	"auto": {}, "en": {}, "es": {}, "fr": {}, "de": {}, "pt": {},
}

var allowedLearningDisabilities = []string{
	"adhd",
	"dyslexia",
	"dyscalculia",
	"dysgraphia",
	"dyspraxia",
	"auditory_processing",
	"autism_spectrum",
	"executive_function",
	"other",
	"prefer_not_to_say",
}

var allowedLearningDisabilitySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(allowedLearningDisabilities))
	for _, k := range allowedLearningDisabilities {
		m[k] = struct{}{}
	}
	return m
}()

func normalizePrefsV1(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("prefs required")
	}
	if len(raw) > 64<<10 {
		return nil, fmt.Errorf("prefs too large")
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid prefs json: %w", err)
	}

	ver, ok := anyInt(obj["version"])
	if !ok || ver != 1 {
		return nil, fmt.Errorf("unsupported prefs version")
	}

	out := map[string]any{}
	out["version"] = 1

	out["nickname"] = sliceString(strings.TrimSpace(anyStr(obj["nickname"])), 64)
	out["occupation"] = sliceString(strings.TrimSpace(anyStr(obj["occupation"])), 120)
	out["about"] = sliceString(strings.TrimSpace(anyStr(obj["about"])), 2400)

	lang := strings.ToLower(strings.TrimSpace(anyStr(obj["language"])))
	if _, ok := allowedPersonalizationLanguages[lang]; !ok {
		lang = "auto"
	}
	out["language"] = lang

	tzMode := strings.ToLower(strings.TrimSpace(anyStr(obj["timezoneMode"])))
	if tzMode != "manual" && tzMode != "auto" {
		tzMode = "auto"
	}
	out["timezoneMode"] = tzMode

	tz := strings.TrimSpace(anyStr(obj["timezone"]))
	if tz == "" {
		tz = "UTC"
	}
	out["timezone"] = sliceString(tz, 80)

	units := strings.ToLower(strings.TrimSpace(anyStr(obj["units"])))
	if units != "imperial" && units != "metric" {
		units = "metric"
	}
	out["units"] = units

	mathComfort := strings.ToLower(strings.TrimSpace(anyStr(obj["mathComfort"])))
	if mathComfort != "low" && mathComfort != "medium" && mathComfort != "high" {
		mathComfort = "medium"
	}
	out["mathComfort"] = mathComfort

	codingComfort := strings.ToLower(strings.TrimSpace(anyStr(obj["codingComfort"])))
	if codingComfort != "none" && codingComfort != "some" && codingComfort != "high" {
		codingComfort = "some"
	}
	out["codingComfort"] = codingComfort

	sessionMinutes := 30
	if v, ok := anyInt(obj["sessionMinutes"]); ok {
		switch v {
		case 10, 15, 20, 30, 45, 60, 90:
			sessionMinutes = v
		}
	}
	out["sessionMinutes"] = sessionMinutes

	sessionsPerWeek := 4
	if v, ok := anyInt(obj["sessionsPerWeek"]); ok {
		switch v {
		case 1, 2, 3, 4, 5, 6, 7, 10, 14:
			sessionsPerWeek = v
		}
	}
	out["sessionsPerWeek"] = sessionsPerWeek

	learningDisabilities := anyStringArray(obj["learningDisabilities"])
	unique := map[string]struct{}{}
	for _, k := range learningDisabilities {
		if _, ok := allowedLearningDisabilitySet[k]; !ok {
			continue
		}
		unique[k] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for _, k := range allowedLearningDisabilities {
		if _, ok := unique[k]; ok {
			ordered = append(ordered, k)
		}
	}
	if len(ordered) > 0 {
		for _, k := range ordered {
			if k == "prefer_not_to_say" {
				ordered = []string{"prefer_not_to_say"}
				break
			}
		}
	}
	out["learningDisabilities"] = ordered

	ldOther := ""
	for _, k := range ordered {
		if k == "other" {
			ldOther = sliceString(strings.TrimSpace(anyStr(obj["learningDisabilitiesOther"])), 280)
			break
		}
	}
	out["learningDisabilitiesOther"] = ldOther

	defaultDepth := strings.ToLower(strings.TrimSpace(anyStr(obj["defaultDepth"])))
	if defaultDepth != "concise" && defaultDepth != "standard" && defaultDepth != "thorough" {
		defaultDepth = "standard"
	}
	out["defaultDepth"] = defaultDepth

	defaultTeachingStyle := strings.ToLower(strings.TrimSpace(anyStr(obj["defaultTeachingStyle"])))
	if defaultTeachingStyle != "balanced" && defaultTeachingStyle != "direct" && defaultTeachingStyle != "socratic" {
		defaultTeachingStyle = "balanced"
	}
	out["defaultTeachingStyle"] = defaultTeachingStyle

	defaultTone := strings.ToLower(strings.TrimSpace(anyStr(obj["defaultTone"])))
	if defaultTone != "neutral" && defaultTone != "encouraging" && defaultTone != "no_fluff" {
		defaultTone = "neutral"
	}
	out["defaultTone"] = defaultTone

	defaultPractice := strings.ToLower(strings.TrimSpace(anyStr(obj["defaultPractice"])))
	if defaultPractice != "light" && defaultPractice != "balanced" && defaultPractice != "more" {
		defaultPractice = "balanced"
	}
	out["defaultPractice"] = defaultPractice

	out["preferShortParagraphs"] = anyBool(obj["preferShortParagraphs"], false)
	out["preferBulletSummaries"] = anyBool(obj["preferBulletSummaries"], true)
	out["askClarifyingQuestions"] = anyBool(obj["askClarifyingQuestions"], true)

	out["allowBehaviorPersonalization"] = anyBool(obj["allowBehaviorPersonalization"], true)
	out["allowTelemetry"] = anyBool(obj["allowTelemetry"], true)

	// Keep output stable and minimal.
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (us *userService) UpsertPersonalizationPrefs(ctx context.Context, prefsJSON []byte) (*types.UserPersonalizationPrefs, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}
	if us.prefsRepo == nil {
		return nil, fmt.Errorf("prefs repo not configured")
	}

	normalized, err := normalizePrefsV1(prefsJSON)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	row := &types.UserPersonalizationPrefs{
		ID:        uuid.New(),
		UserID:    rd.UserID,
		PrefsJSON: datatypes.JSON(normalized),
		UpdatedAt: now,
	}

	var out *types.UserPersonalizationPrefs
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if err := us.prefsRepo.Upsert(dbc, row); err != nil {
			return err
		}
		got, err := us.prefsRepo.GetByUserID(dbc, rd.UserID)
		if err != nil {
			return err
		}
		out = got
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}
