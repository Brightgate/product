/*
 * License: MIT Open Source
 * Copyright (c) Joe Linoff 2016
 * Wrap around golang.org/x/crypto/ssh/terminal to handle ^C interrupts
 * based on a suggestion by Konstantin Shaposhnikov in
 * this thread: https://groups.google.com/forum/#!topic/golang-nuts/kTVAbtee9UA.
 * Correctly resets terminal echo after ^C interrupts.
 *
 * Source: https://gist.github.com/jlinoff/e8e26b4ffa38d379c7f1891fd174a6d0
 */

package main

import (
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"os"
	"os/signal"
	"syscall"
)

func getPassword(prompt string) (string, error) {
	// Get the initial state of the terminal.
	initialTermState, e1 := terminal.GetState(syscall.Stdin)
	if e1 != nil {
		return "", e1
	}

	// Restore it in the event of an interrupt.
	// CITATION: Konstantin Shaposhnikov - https://groups.google.com/forum/#!topic/golang-nuts/kTVAbtee9UA
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill) // Kill may not be caught
	go func() {
		<-c
		_ = terminal.Restore(syscall.Stdin, initialTermState)
		os.Exit(1)
	}()

	// Now get the password.
	fmt.Print(prompt)
	p, err := terminal.ReadPassword(syscall.Stdin)
	fmt.Println("")
	if err != nil {
		return "", err
	}

	// Stop looking for ^C on the channel.
	signal.Stop(c)

	// Return the password as a string.
	return string(p), nil
}
