package steps

import (
	"testing"

	"github.com/google/uuid"
)

func TestSignatureForConceptIDsStable(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	sigA := signatureForConceptIDs(ids)
	if sigA == "" {
		t.Fatalf("expected non-empty signature")
	}
	sigB := signatureForConceptIDs([]uuid.UUID{ids[2], ids[0], ids[1]})
	if sigA != sigB {
		t.Fatalf("expected stable signature, got %s and %s", sigA, sigB)
	}
}

func TestPromotionDecision(t *testing.T) {
	promoteMastery := 0.80
	promoteConf := 0.60
	demoteMastery := 0.65
	demoteConf := 0.50

	if got := promotionDecision(false, 0.9, 0.9, false, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "promote" {
		t.Fatalf("expected promote, got %s", got)
	}
	if got := promotionDecision(false, 0.4, 0.4, false, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "skip" {
		t.Fatalf("expected skip, got %s", got)
	}
	if got := promotionDecision(false, 0.9, 0.9, true, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "skip" {
		t.Fatalf("expected skip with misconception, got %s", got)
	}
	if got := promotionDecision(true, 0.9, 0.9, false, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "keep" {
		t.Fatalf("expected keep, got %s", got)
	}
	if got := promotionDecision(true, 0.6, 0.9, false, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "demote" {
		t.Fatalf("expected demote on low mastery, got %s", got)
	}
	if got := promotionDecision(true, 0.9, 0.4, false, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "demote" {
		t.Fatalf("expected demote on low confidence, got %s", got)
	}
	if got := promotionDecision(true, 0.9, 0.9, true, promoteMastery, promoteConf, demoteMastery, demoteConf); got != "demote" {
		t.Fatalf("expected demote on misconception, got %s", got)
	}
}
