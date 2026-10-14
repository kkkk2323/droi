package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
)

func TestTheSidebarSwitchesComputersFromItsTopLeft(t *testing.T) {
	box, mini := startOtherComputer(t, boxSessions(), ""), startMacMini(t)
	rl := &relaunches{}
	h := thisComputer(t, rl, func(cfg *Config) {
		computer.Follow(cfg.Prefs, box.paired())
		computer.Follow(cfg.Prefs, mini.paired())
		computer.UseThisComputer(cfg.Prefs)
	})
	h.showsOnly("On this Mac", "On the box", "On the mini")

	h.click("Computer: This computer")
	if err := h.tt.ChooseMenuItem("Ubuntu box"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.showsOnly("On the box", "On this Mac", "On the mini")
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		writePNG(filepath.Join(dir, "computer-menu.png"), h.tt.Image())
	}

	h.click("Computer: Ubuntu box")
	if err := h.tt.ChooseMenuItem("Mac mini"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.showsOnly("On the mini", "On the box", "On this Mac")

	h.click("Computer: Mac mini")
	if err := h.tt.ChooseMenuItem("This computer"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.showsOnly("On this Mac", "On the box", "On the mini")
	if rl.n != 0 || h.switches != 3 {
		t.Fatalf("relaunched %d times, switched %d", rl.n, h.switches)
	}
}

func TestTheComputerMenuOpensTheComputerSettings(t *testing.T) {
	box := startOtherComputer(t, boxSessions(), "")
	h := thisComputer(t, &relaunches{}, func(cfg *Config) {
		computer.Follow(cfg.Prefs, box.paired())
		computer.UseThisComputer(cfg.Prefs)
	})
	h.showsOnly("On this Mac", "On the box")
	h.click("Computer: This computer")
	if err := h.tt.ChooseMenuItem("Manage computers…"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "computer" {
		t.Fatalf("the window shows %+v", r)
	}
	if h.switches != 0 {
		t.Fatalf("switched %d times", h.switches)
	}
}

func TestNoComputerMenuWithoutAnotherComputer(t *testing.T) {
	h := thisComputer(t, &relaunches{}, nil)
	h.showsOnly("On this Mac")
	if h.hasButton("Computer: This computer") {
		t.Fatal("a computer menu with nothing to switch to")
	}
}
