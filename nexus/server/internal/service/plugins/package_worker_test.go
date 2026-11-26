package plugins

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--plugin-worker" {
		run := runPackageWorker
		if runtime.GOOS == "linux" {
			run = RunPackageWorker
		}
		if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPackageWorkerDiscardsAFailedProcessAndKeepsAcceptingCommands(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	normal := fixtureWASM(`{"message":"child reply"}`, nil)
	if _, err := callPackageWorker(ctx, executable, workerCompile, normal, nil); err != nil {
		t.Fatalf("worker compile: %v", err)
	}
	if _, err := callPackageWorker(ctx, executable, workerCompile, []byte("invalid"), nil); err == nil {
		t.Fatal("worker accepted invalid code")
	}
	loop := []byte{0x03, 0x40, 0x0c, 0x00, 0x0b} // loop { br 0 }
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := callPackageWorker(short, executable, workerExecute, fixtureWASM("", loop), []byte("{}")); err == nil ||
		!strings.Contains(err.Error(), "超时") {
		t.Fatalf("parent failed to stop unresponsive child: %v", err)
	}
	result, err := callPackageWorker(ctx, executable, workerExecute, normal, []byte("{}"))
	if err != nil || result.Message != "child reply" {
		t.Fatalf("worker recovery: %+v, %v", result, err)
	}
}
