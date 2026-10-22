package main

import (
	"os"
	"os/exec"
)

type Runner interface {
	Run(args ...string) error
	Output(args ...string) ([]byte, error)
}
type RealRunner struct{}

func (RealRunner) Run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
func (RealRunner) Output(args ...string) ([]byte, error) {
	return exec.Command(args[0], args[1:]...).CombinedOutput()
}
