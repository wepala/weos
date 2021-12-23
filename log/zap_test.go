package logs

import (
	"github.com/labstack/gommon/log"
	"testing"
)

func TestZap_Level(t *testing.T) {
	zlogger, err := NewZap("debug")
	if err != nil {
		t.Fatalf("unexpected error setting up logger '%s'", err)
	}
	defer zlogger.Sync() // flushes buffer, if any
	if zlogger.Level() != log.DEBUG {
		t.Errorf("expected the level to be %d got %d", log.DEBUG, zlogger.Level())
	}
}

func TestZap_SetLevel(t *testing.T) {
	zlogger, err := NewZap("info")
	if err != nil {
		t.Fatalf("unexpected error setting up logger '%s'", err)
	}
	defer zlogger.Sync() // flushes buffer, if any
	zlogger.SetLevel(log.ERROR)
	if zlogger.Level() != log.ERROR {
		t.Errorf("expected the level to be %d got %d", log.ERROR, zlogger.Level())
	}
}
