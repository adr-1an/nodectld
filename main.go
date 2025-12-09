package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mdlayher/vsock"
)

func getPort() uint32 {
	data, err := os.ReadFile("/.nodectld-port")
	if err != nil {
		// File doesn't exist, default to port 1
		return 1
	}

	p, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || p <= 0 {
		// Invalid content -> default
		return 1
	}

	return uint32(p)
}

func main() {
	port := getPort()
	ln, err := vsock.Listen(port, nil)
	if err != nil {
		log.Fatalf("[FATAL] Failed to listen: %v", err)
	}

	fmt.Printf("[OK] NodeCTLD listening on port %d\n", port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}

		go handle(conn)
	}
}

func handle(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()

	r := bufio.NewReader(conn)

	// read the command line
	cmdStr, err := r.ReadString('\n')
	if err != nil {
		return
	}
	cmdStr = strings.TrimSpace(cmdStr)

	if cmdStr == "" {
		return
	}

	// basic protocol extensions
	parts := strings.Split(cmdStr, " ")
	switch parts[0] {

	// UPLOAD <path> <size>
	case "UPLOAD":
		if len(parts) != 3 {
			_, _ = fmt.Fprintln(conn, "ERR invalid upload syntax")
			return
		}

		path := parts[1]
		size, err := strconv.Atoi(parts[2])
		if err != nil || size < 0 {
			_, _ = fmt.Fprintln(conn, "ERR invalid size")
			return
		}

		// create file
		f, err := os.Create(path)
		if err != nil {
			_, _ = fmt.Fprintf(conn, "ERR cannot create file: %v\n", err)
			return
		}
		defer func() { _ = f.Close() }()

		// copy N bytes exactly
		n, err := io.CopyN(f, r, int64(size))
		if err != nil {
			_, _ = fmt.Fprintf(conn, "ERR upload failed after %d bytes: %v\n", n, err)
			return
		}

		_, _ = fmt.Fprintf(conn, "[OK] uploaded %d bytes\n", n)
		return

	// READ <path>
	case "READ":
		if len(parts) != 2 {
			_, _ = fmt.Fprintln(conn, "ERR invalid read syntax")
			return
		}

		path := parts[1]
		f, err := os.Open(path)
		if err != nil {
			_, _ = fmt.Fprintf(conn, "ERR cannot open file: %v\n", err)
			return
		}
		defer func() { _ = f.Close() }()

		// stream file to host
		_, _ = io.Copy(conn, f)
		return
	}

	// Default: execute shell cmd

	// Run cmd
	cmd := exec.Command("/bin/sh", "-c", cmdStr)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(conn, "error starting cmd: %v\n", err)
		return
	}

	go func() { _, _ = io.Copy(conn, stdout) }()
	go func() { _, _ = io.Copy(conn, stderr) }()

	_ = cmd.Wait()
}
