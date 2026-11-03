package config

import "testing"

func TestSessionToolsUpgradeAndExplicitDisable(t *testing.T) {
	c := Default()
	c.Tools = c.Tools[:2]
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 4 {
		t.Fatal("existing configuration missed session tools")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 4 {
		t.Fatal("duplicate tools")
	}
	c.Tools = nil
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 0 {
		t.Fatal("explicit tool disable overwritten")
	}
}
