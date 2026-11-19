// Package io is the Go behind Kogo's package kogo.io, in prelude/io.kg.
package io

import (
	"bufio"
	"fmt"
	goio "io"
	"strings"

	"github.com/Cloud-Limo/kogo/rt"
)

// A Console is a program's standard input and output.
type Console struct {
	in  *bufio.Reader
	out goio.Writer
}

func NewConsole(in goio.Reader, out goio.Writer) Console { return Console{bufio.NewReader(in), out} }

func Console_Println(this Console, message string) rt.Unit {
	return Console_Print(this, message+"\n")
}

// Console_Print drops a failed write, as Kotlin's System.out does: a
// closed or full output does not stop the program.
func Console_Print(this Console, message string) rt.Unit {
	goio.WriteString(this.out, message)
	return rt.Unit{}
}

// Console_Readln is the next line without its line break, \n or \r\n;
// past the end of the input it panics, as Kotlin's throws.
func Console_Readln(this Console) string {
	line, err := this.in.ReadString('\n')
	if err != nil && (err != goio.EOF || line == "") {
		panic(fmt.Sprintf("readln: %v", err))
	}
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}
