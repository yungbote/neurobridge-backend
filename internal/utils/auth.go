package utils

import (
  "context"
  "fmt"
  "golang.org/x/crypto/bcrypt"
  "github.com/yungbote/neurobridge-backend/internal/normalization"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/repos"
)

func InputValidation(ctx context.Context, ffor string, userRepo repos.UserRepo, log *logger.Logger, user *types.User, email, password string) error {
  validatedFor := normalization.ParseInputString(ffor)
  if validatedFor == "" {
    return fmt.Errorf("For string is nil, needs to be login or registration")
  }
  switch validatedFor {
  case "registration":
    if err := handleRegisterInputValidation(ctx, userRepo, log, user); err != nil {
      return err
    }
  case "login":
    if err := handleLoginInputValidation(ctx, log, email, password); err != nil {
      return err
    }
  }
  return nil
}

func handleRegisterInputValidation(ctx context.Context, userRepo repos.UserRepo, log *logger.Logger, user *types.User) error {
  if user == nil {
    return fmt.Errorf("No user given, cannot proceed with registration")
  }
  if user.Email == "" {
    return fmt.Errorf("An email is required to register")
  }
  emailExists, err := userRepo.EmailExists(ctx, nil, user.Email)
  if err != nil {
    return fmt.Errorf("Failed to check user email")
  }
  if emailExists {
    return fmt.Errorf("Email is already in use")
  }
  if user.Password == "" {
    return fmt.Errorf("A password is required to register")
  }
  if user.FirstName == "" {
    return fmt.Errorf("A first name is required to register")
  }
  if user.LastName == "" {
    return fmt.Errorf("A last name is required to register")
  }
  return nil
}

func handleLoginInputValidation(ctx context.Context, log *logger.Logger, email, password string) error {
  if email == "" {
    return fmt.Errorf("Email is required to login")
  }
  if password == "" {
    return fmt.Errorf("Password is required to login")
  }
  return nil
}

func HashPassword(ctx context.Context, log *logger.Logger, user *types.User) error {
  hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
  if err != nil {
    return fmt.Errorf("Failed to hash password")
  }
  user.Password = string(hashedPassword)
  return nil
}

func NormalizeUserFields(ctx context.Context, user *types.User) {
  user.Email = normalization.ParseInputString(user.Email)
  user.Password = normalization.ParseInputString(user.Password)
  user.FirstName = normalization.ParseInputString(user.FirstName)
  user.LastName = normalization.ParseInputString(user.LastName)
}
