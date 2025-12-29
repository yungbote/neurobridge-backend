package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type UserService interface {
	GetMe(dbc dbctx.Context) (*types.User, error)

	// NEW
	UpdatePreferredTheme(ctx context.Context, preferredTheme string) (*types.User, error)
	UpdateName(ctx context.Context, firstName, lastName string) (*types.User, error)
	UpdateAvatarColor(ctx context.Context, avatarColor string) (*types.User, error)
	UploadAvatarImage(ctx context.Context, raw []byte) (*types.User, error)
}

type userService struct {
	db            *gorm.DB
	log           *logger.Logger
	userRepo      repos.UserRepo
	avatarService AvatarService
}

func NewUserService(db *gorm.DB, log *logger.Logger, userRepo repos.UserRepo, avatarService AvatarService) UserService {
	serviceLog := log.With("service", "UserService")
	return &userService{
		db:            db,
		log:           serviceLog,
		userRepo:      userRepo,
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

func (us *userService) UpdatePreferredTheme(ctx context.Context, preferredTheme string) (*types.User, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}

	preferredTheme = strings.ToLower(strings.TrimSpace(preferredTheme))
	if preferredTheme != "light" && preferredTheme != "dark" && preferredTheme != "system" {
		return nil, fmt.Errorf("invalid preferred_theme")
	}

	var out *types.User
	if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if err := us.userRepo.UpdatePreferredTheme(dbc, rd.UserID, preferredTheme); err != nil {
			return err
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
