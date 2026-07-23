//go:build !windows

package cortext

import "os"

func setNativeEnv(key, value string) error {
	return os.Setenv(key, value)
}

func unsetNativeEnv(key string) error {
	return os.Unsetenv(key)
}
