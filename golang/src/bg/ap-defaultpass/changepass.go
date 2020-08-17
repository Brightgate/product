/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Error types for SSH password resets

// PromptFailureError happens when a readPrompt encounters a "failure" prompt
type PromptFailureError struct {
	FailedPrompt string
}

func (p PromptFailureError) Error() string {
	return fmt.Sprintf("Matched failure prompt \"%s\"", p.FailedPrompt)
}

// PromptNotFoundError happens when no prompts (success or failure) are seen
//     In certain cases (Darwin 'passwd') this is a "no output" success path
type PromptNotFoundError struct {
}

func (p PromptNotFoundError) Error() string {
	return "Prompt not found"
}

// PromptTimeoutError happens when we timeout, usually because something's wrong
type PromptTimeoutError struct {
}

func (p PromptTimeoutError) Error() string {
	return "Timeout waiting for prompt"
}

// UnsupportedOSError is returned if we don't support the target OS
type UnsupportedOSError struct {
}

func (p UnsupportedOSError) Error() string {
	return "Operating system not supported"
}

// SendLineError is returned if we get an error sending input
type SendLineError struct {
	Message string
}

func (p SendLineError) Error() string {
	return p.Message
}

// readPrompt
//
// Reads from the provided reader until timeout (seconds) occurs or
// the reader outputs one of the provided prompts.
//
// If it matches a success prompt, returns nil, otherwise an error with the matched prompt.
//
// TODO: Make robust if more than 1024 bytes of cruft come across the wire
//
func readPrompt(reader *bufio.Reader, success, failure []string, timeout int) error {
	log.Printf("readPrompt looking for: %#v\n", success)
	buffer := make([]byte, 1024)

	ch := make(chan error, 1)
	defer close(ch)

	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()

	go func() {
		_, err := reader.Read(buffer)
		for err == nil {
			for _, succ := range success {
				if strings.Contains(string(buffer), succ) {
					ch <- nil
					return
				}
			}
			for _, fail := range failure {
				if strings.Contains(string(buffer), fail) {
					ch <- PromptFailureError{fail}
					return
				}
			}
			_, err = reader.Read(buffer)
		}
		ch <- PromptNotFoundError{}
	}()

	var err error
	select {
	case result := <-ch:
		err = result
	case <-timer.C:
		err = PromptTimeoutError{}
	}

	log.Printf("readPrompt returning: %#v\n", err)
	return err
}

// sendLine
// This is just a wrapper for writing to remote stdin
func sendLine(writer *io.WriteCloser, line string) error {
	if _, err := io.WriteString(*writer, line); err != nil {
		return SendLineError{fmt.Sprintf("Write error: %v", err)}
	}
	return nil
}

// addDefaultPort
//
// golang net package "address"es require a port, so SSHResetPassword does too.
// If you have an address that may have a port but you don't know, provide
// the address and the default port and it will add the default port iff
// the address doesn't already specify a port.
//
// Example: SSHResetPassword(addDefaultPort(address, "22"), user, oldpw, newpw)
//
func addDefaultPort(address string, port string) string {
	hostport := strings.Split(address, ":")
	if len(hostport) < 1 || len(hostport) > 2 {
		log.Fatalf("Invalid address: '%s'", address)
	} else if len(hostport) == 1 {
		return fmt.Sprintf("%s:%s", address, port)
	}
	return address
}

func gnuLinuxCheckOS(sshClient *ssh.Client) error {
	sshi, err := sshSessionSetup(sshClient, "uname -a")
	if err != nil {
		return err
	}
	defer sshi.Close()

	if err = readPrompt(sshi.Output, []string{"GNU/Linux"}, []string{}, 1); err != nil {
		return err
	}
	return nil
}

func gnuLinuxPasswordReset(sshClient *ssh.Client, prev, next string) error {
	var err error
	var sshi *sshSessionInfo
	if sshi, err = sshSessionSetup(sshClient, "passwd"); err != nil {
		return err
	}
	defer sshi.Close()

	if err = readPrompt(sshi.Output,
		[]string{"(current) UNIX password:"},
		[]string{},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", prev)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{"Enter new UNIX password:"},
		[]string{"Authentication token manipulation error"},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", next)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{"Retype new UNIX password:"},
		[]string{},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", next)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{"successfully"},
		[]string{"Sorry, passwords do not match",
			"Authentication token manipulation error"},
		5); err != nil {
		return err
	}
	return nil // yay!
}

func macOSCheckOS(sshClient *ssh.Client) error {
	sshi, err := sshSessionSetup(sshClient, "uname -a")
	if err != nil {
		return err
	}
	defer sshi.Close()

	if err = readPrompt(sshi.Output, []string{"Darwin"}, []string{}, 1); err != nil {
		return err
	}
	return nil
}

func macOSPasswordReset(sshClient *ssh.Client, prev, next string) error {
	sshi, err := sshSessionSetup(sshClient, "passwd")
	if err != nil {
		return err
	}
	defer sshi.Close()

	if err = readPrompt(sshi.Output,
		[]string{"Old Password:"},
		[]string{},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", prev)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{"New Password:"},
		[]string{},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", next)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{"Retype New Password:"},
		[]string{},
		5); err != nil {
		return err
	}
	if err = sendLine(sshi.Stdin, fmt.Sprintf("%s\r\n", next)); err != nil {
		return err
	}

	if err = readPrompt(sshi.Output,
		[]string{},
		[]string{"authentication token failure", "try again"},
		5); err != nil {
		// MacOS "passwd" outputs nothing on success, and $? is 0 on success OR failure
		switch err.(type) {
		case PromptNotFoundError:
			return nil
		default:
			return err
		}
	}
	return nil // yay!
}

type resetPasswordOS struct {
	Name        string
	CheckOS     func(*sshSessionInfo) error
	ResetPasswd func(*sshSessionInfo, string, string) error
}

// sshPipes takes an active SSH session; opens the remote stdin, stdout, stderr
// Uses native io.WriteCloser for stdin
// Uses a buffered bufio.Reader for stdout/stderr
func sshPipes(sshSession *ssh.Session) (*io.WriteCloser, *bufio.Reader, error) {
	var err error
	var stdinPipe io.WriteCloser
	var stdoutPipe io.Reader
	var sshStdin *io.WriteCloser
	var sshOutput *bufio.Reader

	if stdinPipe, err = sshSession.StdinPipe(); err != nil {
		return sshStdin, sshOutput, err
	}

	if stdoutPipe, err = sshSession.StdoutPipe(); err != nil {
		return sshStdin, sshOutput, err
	}

	sshStdin = &stdinPipe
	sshOutput = bufio.NewReader(stdoutPipe)
	sshSession.Stderr = sshSession.Stdout // 2>&1 Force everything into Stdout
	return sshStdin, sshOutput, nil
}

func (sshi *sshSessionInfo) Close() {
	sshi.Session.Close()
}

// sshSessionInfo
//
// sshSessionSetup abstracts
// away from the functions that use them.
type sshSessionInfo struct {
	Session *ssh.Session
	Stdin   *io.WriteCloser
	Output  *bufio.Reader
}

// sshSessionSetup
//
// TODO: consider replacing with https://github.com/google/goexpect
// Note it says at the bottom it is "not an official Google product"
// and this library has not made it into awesomego. CAT had trouble
// getting it to play well with some of these and decided to punt.
//
// Given an active SSH client, set up a session with a vt100 pty,
// starts the provided "start" command, and returns a sshSessionInfo
// with session and I/O handles, or sets err.
//
// Callers are expected to .Close() the result when completed.
//
// Note that sshPipes combines stdout and stderr into a single sshOutput,
// since GNU/Linux passwd outputs to both of them and interleaving is a pain.
//
// Bare 'return' means "leave the pipe return values unset, with err set"
//
func sshSessionSetup(sshClient *ssh.Client, cmd string) (*sshSessionInfo, error) {
	var err error
	var sshi *sshSessionInfo
	var sshSession *ssh.Session
	var sshStdin *io.WriteCloser
	var sshOutput *bufio.Reader

	if sshSession, err = sshClient.NewSession(); err != nil {
		return sshi, err
	}

	termModes := ssh.TerminalModes{
		ssh.ECHO:  0, // Disable echoing
		ssh.IGNCR: 1, // Ignore CR on input.
	}
	if err = sshSession.RequestPty("vt100", 80, 24, termModes); err != nil {
		sshSession.Close()
		return sshi, err
	}
	if sshStdin, sshOutput, err = sshPipes(sshSession); err != nil {
		sshSession.Close()
		return sshi, err
	}

	if err = sshSession.Start(cmd); err != nil {
		sshSession.Close()
		return sshi, err
	}

	sshi = &sshSessionInfo{sshSession, sshStdin, sshOutput}
	return sshi, nil
}

// SSHResetPassword resets a foreign host's SSH account
// address: e.g. "host.domain.tld", "localhost",
//               "192.168.123.1" (port 22) or "______:port"
//          see net.Dial -- https://golang.org/pkg/net/?m=all#Dial
// user:    username
// oldPass: old password (current, and to be changed)
// newPass: new password (to be changed to)
//
func SSHResetPassword(address, user, oldPass, newPass string) error {
	sshConfig := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(oldPass)},
	}

	var err error
	var sshClient *ssh.Client

	// Open a new client
	if sshClient, err = ssh.Dial("tcp", address, sshConfig); err != nil {
		return err
	}

	// Currently only support GNU/Linux (pi, debian) and macOS
	//
	if err = gnuLinuxCheckOS(sshClient); err == nil {
		return gnuLinuxPasswordReset(sshClient, oldPass, newPass)
	} else if err = macOSCheckOS(sshClient); err == nil {
		return macOSPasswordReset(sshClient, oldPass, newPass)
	}

	return UnsupportedOSError{}
}

