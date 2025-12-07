package utils

import (
  "os"
  "strconv"
  "github.com/yungbote/neurobridge-backend/internal/logger"
)

func GetEnv(key, defaultVal string, log *logger.Logger) string {
  if log != nil {
    log = log.With("env_var", key)
  }
  val, ok := os.LookupEnv(key)
  if !ok {
    if log != nil {
      log.Debug("Environment variable not found, using default", "default", defaultVal)
    }
    return defaultVal
  }
  if log != nil {
    log.Debug("Environment variable found, using environment", "environment", val)
  }
  return val
}

func GetEnvAsInt(key string, defaultVal int, log *logger.Logger) int {
  if log != nil {
    log = log.With("env_var", key)
  }
  valStr, ok := os.LookupEnv(key)
  if !ok {
    if log != nil {
      log.Debug("Environment variable not found, using default", "default", defaultVal)
    }
    return defaultVal
  }
  i, err := strconv.Atoi(valStr)
  if err != nil {
    if log != nil {
      log.Debug("Environment variable could not be parsed as int, using default", "providedVal", valStr, "defaultVal", defaultVal, "error", err)
    }
    return defaultVal
  }
  if log != nil {
    log.Debug("Environment variable found, using it", "value", i)
  }
  return i
}
