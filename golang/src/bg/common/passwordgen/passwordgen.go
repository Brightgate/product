/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package passwordgen

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
	"math"
	"math/big"
	"unicode"

	"github.com/sethvargo/go-diceware/diceware"
)

// cryptoInt
//
// Use more secure "crypto/rand" to return an int <= maxInt
//
func cryptoInt(maxInt int) int {
	rval, err := rand.Int(rand.Reader, big.NewInt(int64(maxInt)))
	if err != nil {
		// we can't generate trustworthy passwords
		log.Panicf("crypto/rand returned error: %v", err)
	}
	return int(rval.Int64())
}

// randomRune
//
// Return a random element from a list of runes
//
func randomRune(a []rune) rune {
	return a[cryptoInt(len(a))]
}

// PasswordSpec represents a specification for generating a random password
//
// Some systems will have different password requirements.
// https://arstechnica.com/information-technology/2013/04/ ...
// why-your-password-cant-have-symbols-or-be-longer-than-16-characters/
// This PasswordSpec allows vulnerability detectors to
// specify what kinds of passwords are permitted for a
// specific device.
//
// Each of these fields was necessary based on some potential customer's
// required password criteria.
//
// For example, certain Xerox printers only allow 10 character passwords,
// so fully random passwords must be used to achieve plausible entropy.
//
// This interface contemplates wrappers that can avoid confusing
// symbols and digits such as: 1lI|, 0O, +t, \/, etc., for example
//
// If you don't want mixed case, don't include both cases in AllowedLetters.
//
// An empty Allowed array means those characters are not used.
//
// Don't create inconsistent specs, e.g.: AllowedDigits = []{}; NumDigits = 1
//
type PasswordSpec struct {
	NamePrefix     string // prefix of name of this spec, used with String()
	HumanWords     string // "" for random; ISO language code, e.g. "en_US" for words
	AllowedLetters []rune // IGNORED if HumanWords == true
	UpperLower     bool   // Must have both uppercase and lowercase
	AllowedSymbols []rune
	NumSymbols     int // how many symbols are required?
	AllowedDigits  []rune
	NumDigits      int  // how many digits are required?
	MaxLength      int  // how many characters are permitted?
	MinLength      int  // how many characters are required?
	LetterBookends bool // must begin/end with a letter
	TargetEntropy  int  // how many bits of entropy are desired?
}

// WordPasswordSpec has 4.7 bits of entropy per character
// Used if our wordlist fails
var WordPasswordSpec = PasswordSpec{
	NamePrefix: "Word",
	HumanWords: "",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z'},
	AllowedSymbols: []rune{},
	NumSymbols:     0,
	AllowedDigits:  []rune{},
	NumDigits:      0,
	MaxLength:      9,
	TargetEntropy:  32,
}

// LimitedPasswordSpec8 has 5.64 bits of entropy per character
// An 8-char pw = ~45 bits of entropy
//
// Avoids confusing character overlaps like 0,O, 1,I,l, t,+
var LimitedPasswordSpec8 = PasswordSpec{
	NamePrefix: "Limited",
	HumanWords: "",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'm', 'n', 'p', 'q',
		'r', 's', 'u', 'v', 'w', 'x', 'y', 'z', 'A', 'B', 'C', 'D', 'E', 'F', 'G',
		'H', 'J', 'K', 'M', 'N', 'P', 'Q', 'R', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z'},
	AllowedSymbols: []rune{'-', '_'},
	NumSymbols:     0,
	AllowedDigits:  []rune{'2', '3', '4', '6', '7', '8', '9'},
	NumDigits:      0,
	MaxLength:      10,
	TargetEntropy:  44,
}

// LimitedPasswordSpec10 has 5.64 bits of entropy per character
// A 10-char pw = ~50 bits with 1 required digit + 1 symbol
//
// Avoids confusing character overlaps like 0,O, 1,I,l, t,+
var LimitedPasswordSpec10 = PasswordSpec{
	NamePrefix: "Limited",
	HumanWords: "",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'm', 'n', 'p', 'q',
		'r', 's', 'u', 'v', 'w', 'x', 'y', 'z', 'A', 'B', 'C', 'D', 'E', 'F', 'G',
		'H', 'J', 'K', 'M', 'N', 'P', 'Q', 'R', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z'},
	AllowedSymbols: []rune{'-', '_'},
	NumSymbols:     1,
	AllowedDigits:  []rune{'2', '3', '4', '6', '7', '8', '9'},
	NumDigits:      1,
	MaxLength:      10,
	TargetEntropy:  54,
}

// FlexiblePasswordSpec excludes ' ' and '\'' to avoid bugs (93 characters)
// 6.54 bits of entropy per character
//
// A 20-char pw = ~118 bits with 2 required digits + 2 symbols
var FlexiblePasswordSpec = PasswordSpec{
	NamePrefix: "Flexible",
	HumanWords: "",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
		'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z'},
	AllowedSymbols: []rune{
		'!', '@', '#', '$', '%', '&', '(', ')', '*', '+', ',', '-', '.',
		'/', ':', ';', '<', '=', '>', '?', '"', '[', '\\', ']', '^', '_',
		'`', '{', '|', '}', '~'},
	NumSymbols:    2,
	AllowedDigits: []rune{'0', '1', '2', '3', '4', '6', '7', '8', '9'},
	NumDigits:     2,
	MaxLength:     20,
	TargetEntropy: 110,
}

// SecurityTheaterPasswordSpec is based on an actual password spec required
// by a cybersecurity company in 2018.
//
// excludes ' ' and '\'' to avoid bugs (93 characters)
// 6.54 bits of entropy per character
//
// A 20-char pw = ~118 bits with 2 required digits + 2 symbols
//
// TODO: check to see if result contains a dictionary word
// TODO: verify allowed symbols are all permitted
//
var SecurityTheaterPasswordSpec = PasswordSpec{
	NamePrefix: "SecurityTheater",
	HumanWords: "",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
		'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z'},
	AllowedSymbols: []rune{
		'!', '@', '#', '$', '%', '&', '(', ')', '*', '+', ',', '-', '.',
		'/', ':', ';', '<', '=', '>', '?', '"', '[', '\\', ']', '^', '_',
		'`', '{', '|', '}', '~'},
	NumSymbols:     2,
	AllowedDigits:  []rune{'0', '1', '2', '3', '4', '6', '7', '8', '9'},
	NumDigits:      1,
	MinLength:      12,
	MaxLength:      20,
	UpperLower:     true,
	LetterBookends: true,
	TargetEntropy:  110,
}

// HumanPasswordSpec is designed for the EFF dice list for US English and
// about 40 characters of space
//
// The EFF list is 7776 words with a max of 9 characters
// ~12.9 bits of entropy per word, all lowercase
// ~3 bits of entropy per digit
// 0 bits of entropy per symbol; using '-' not ' ' for broader compatibility
//
// Minimum: 4 words + 1 digit = ~ 55 bits of entropy
var HumanPasswordSpec = PasswordSpec{
	NamePrefix: "Human",
	HumanWords: "en_US",
	AllowedLetters: []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z'},
	AllowedSymbols: []rune{'-'},
	NumSymbols:     2,
	AllowedDigits:  []rune{},
	NumDigits:      0,
	MaxLength:      40,
	TargetEntropy:  50,
}

// PasswordEntropyError tells the caller that the TargetEntropy was not reached
type PasswordEntropyError struct{ message string }

func (err PasswordEntropyError) Error() string { return err.message }

// EntropyPassword tries to maximize entropy given the rules
//
// This and PasswordSpec were inspired by sethvargo/go-password.
// Because that implementation segregates symbols and numbers from short
// passwords, 8-10 character limited passwords don't have enough entropy.
// This is intended to handle the SecurityTheaterPasswordSpec
//
func EntropyPassword(spec PasswordSpec) (string, error) {
	if spec.HumanWords != "" {
		return HumanPassword(spec)
	}
	allowedRunes := append(spec.AllowedLetters, spec.AllowedDigits...)
	allowedRunes = append(allowedRunes, spec.AllowedSymbols...)
	entropy := float64(0)

	symsNeeded := spec.NumSymbols
	digsNeeded := spec.NumDigits
	seenUpper := false
	seenLower := false
	endsLetter := 0 // {0,2} (not bool), we need to reserve 2 characters for each end
	if spec.LetterBookends || spec.UpperLower {
		endsLetter = 2
	}

	var err error
	passBuf := make([]byte, 0, spec.MaxLength)
	pass := bytes.NewBuffer(passBuf)
	for entropy < float64(spec.TargetEntropy) &&
		(pass.Len()+endsLetter) < spec.MaxLength {
		idx := cryptoInt(len(allowedRunes))
		// Relies on ordering of AllowedLetters, AllowedSymbols, AllowedDigits
		if idx >= len(spec.AllowedLetters) {
			if idx < len(spec.AllowedLetters)+len(spec.AllowedSymbols) {
				symsNeeded--
			} else {
				digsNeeded--
			}
		} else if allowedRunes[idx] == unicode.ToUpper(allowedRunes[idx]) {
			seenUpper = true
		} else {
			seenLower = true
		}
		if _, err = pass.WriteRune(allowedRunes[idx]); err != nil {
			return "", err
		}
		entropy += math.Log2(float64(len(allowedRunes)))
	}

	// At this point we are guaranteed to have symsNeeded + digsNeeded bytes
	// _OR_ we hit our target entropy and we don't need more bytes
	// symsNeeded, digsNeeded could be negative if selected > minimum num/sym times
	for i := 0; i < symsNeeded; i++ {
		if (pass.Len() + endsLetter) < spec.MaxLength {
			_, err = pass.WriteRune(randomRune(spec.AllowedSymbols))
			if err != nil {
				return "", err
			}
			entropy += math.Log2(float64(len(spec.AllowedSymbols)))
		}
	}
	for i := 0; i < digsNeeded; i++ {
		if (pass.Len() + endsLetter) < spec.MaxLength {
			_, err = pass.WriteRune(randomRune(spec.AllowedDigits))
			if err != nil {
				return "", err
			}
			entropy += math.Log2(float64(len(spec.AllowedDigits)))
		}
	}

	if endsLetter > 0 {
		// add letter at end, handle UpperLower
		r := randomRune(spec.AllowedLetters)
		ent := math.Log2(float64(len(spec.AllowedLetters)))
		if spec.UpperLower && !(seenUpper && seenLower) {
			ent = ent / 2 // assumes same # of upper/lower letters
			if seenUpper {
				r = unicode.ToLower(r)
			} else {
				r = unicode.ToUpper(r)
			}
		}
		_, err = pass.WriteRune(r)
		if err != nil {
			return "", err
		}
		entropy += ent
		// add letter at beginning
		s := randomRune(spec.AllowedLetters)
		currString := pass.String()
		pass.Reset()
		_, err = pass.WriteRune(s)
		if err != nil {
			return "", err
		}
		_, err = pass.WriteString(currString)
		if err != nil {
			return "", err
		}
		entropy += ent
	}

	if entropy < float64(spec.TargetEntropy) {
		// Let the caller decide whether they care
		err = PasswordEntropyError{
			fmt.Sprintf("%.2f < %d target entropy",
				entropy, spec.TargetEntropy)}
	}
	return pass.String(), err
}

// rollDiceware
//
// Wrapper resilient to various errors to pull a random word
// from sethvargo's implementation of EFF diceware passwords
func rollDiceware() (string, error) {
	var rval string
	if words, err := diceware.Generate(1); err != nil {
		log.Printf("diceware.Generate error: %v", err)
		var word string
		if word, err = EntropyPassword(WordPasswordSpec); err != nil {
			log.Printf("password.Generate failed: %v", err)
			return "", err
		}
		rval = word
	} else {
		rval = words[0]
	}
	return rval, nil
}

// rollDicewareEntropy estimates each word at log2(7776) (6^5)
const rollDicewareEntropy = float64(12.9)

// String produces a printable or recordable description of the spec (n.b. it
// is not a complete description).
func (spec *PasswordSpec) String() string {
	return fmt.Sprintf("%s-%d-%d", spec.NamePrefix, spec.MaxLength, spec.TargetEntropy)
}

// HumanPassword is based on EFF diceware but to also satisfy password
// "security" rules that may require a special character and an integer
//
// nWords:  # of words you want (from EFF "long" list of 7776 <=9 char words)
//          https://www.eff.org/dice
//          https://www.eff.org/files/2016/07/18/eff_large_wordlist.txt
// symbols: an array of valid symbols (runes)
// nDigits: how many digits you want at the end
//
// Excludes numeric values with repeated digits or sequences
//
// With 3 words, you only get 38.8 bits
// With 4 words, you only get 51.7 bits (barely enough)
// With 5 words, you get 64.6 bits
// With 6 words, you get 77.5 bits
// Digits and symbols add about 1 bit each (worse than words, per byte)
//        ... but they satisfy "secure" password requirements :-P
//
// Since you need 40 characters for 4 words, errors on MaxLength < 40
//
// Method:
// Start with a digit
// Repeat until Stop:
//     Choose a random word from EFF list
//     If adding the word would not exceed MaxLength:
//         Add the word plus a random symbol from AllowedSymbols
//     Else
//         Stop
// Pad with digits
//
// A password of length 40 will have at least:
// 4 words, 3 symbols.
//
// TODO: Non-English (see https://www.rempe.us/diceware/#eff, but Spanish is terribad)
//
func HumanPassword(spec PasswordSpec) (newPass string, err error) {
	const maxWordLen = 9
	const neededWords = 4 // for adequate entropy
	if spec.HumanWords == "" || (spec.NumSymbols > 0 && len(spec.AllowedSymbols) < 1) {
		err = fmt.Errorf("HumanPassword received unfriendly spec: %#v", spec)
		return
	} else if spec.HumanWords != "en_US" {
		err = fmt.Errorf("HumanPassword only supports US English: %#v", spec)
		return
	}
	// This basically comes out to a MaxLength of ~40
	if spec.MaxLength < maxWordLen*neededWords+spec.NumSymbols+spec.NumDigits {
		err = fmt.Errorf("MaxLength too small for HumanWords")
		return
	}

	entropy := float64(0)
	passBuf := make([]byte, 0, spec.MaxLength)
	pass := bytes.NewBuffer(passBuf)
	var word string
	if word, err = rollDiceware(); err != nil {
		return // rollDiceware complained
	}

	symsNeeded := spec.NumSymbols
	digsNeeded := spec.NumDigits

	for pass.Len()+len(word) < spec.MaxLength {
		pass.WriteString(word)
		if len(spec.AllowedSymbols) > 0 {
			pass.WriteRune(randomRune(spec.AllowedSymbols))
			entropy += math.Log2(float64(len(spec.AllowedSymbols)))
			symsNeeded--
		} else if len(spec.AllowedDigits) > 0 {
			// We never hit this case with HumanPasswordSpec
			pass.WriteRune(randomRune(spec.AllowedDigits))
			entropy += math.Log2(float64(len(spec.AllowedDigits)))
			digsNeeded--
		}
		if word, err = rollDiceware(); err != nil {
			return // rollDiceware complained
		}
		entropy += rollDicewareEntropy
	}
	prev := 'x' // will not trigger checks
	for len(spec.AllowedDigits) > 0 && pass.Len() < spec.MaxLength {
		next := randomRune(spec.AllowedDigits)
		if next != prev && next != prev+1 && next != prev-1 {
			pass.WriteRune(next)
			prev = next
			entropy += math.Log2(math.Min(
				float64(len(spec.AllowedDigits)-3), 1.0))
			// this is a lower bound on the entropy
		}
	}
	// remove extraneous '-' at the end, if present
	result := pass.String()
	if result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	return result, nil
}
