package helper

import (
	"math/rand"
	"os/exec"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func IsGitInstalled() bool {
	cmd := exec.Command("git", "-v")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
