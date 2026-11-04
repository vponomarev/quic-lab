package serverexit

import "testing"

func TestPolicyAllowsExplicitChoiceWithoutDefault(t *testing.T) {
	c, _ := NewCatalog(testDefinitions())
	p := &Policy{Allowed: []string{"west", "east"}}
	if err := c.ValidatePolicy(p); err != nil {
		t.Fatal("administrator cannot require explicit selection", err)
	}
	if _, err := c.Resolve(p, ""); err == nil {
		t.Fatal("missing default fell back")
	}
	if id, err := c.Resolve(p, "east"); err != nil || id != "east" {
		t.Fatal(id, err)
	}
}
func TestRemovedExitDoesNotBlockOtherExplicitChoice(t *testing.T) {
	c, _ := NewCatalog(testDefinitions()[:1])
	p := &Policy{Allowed: []string{"west", "east"}, Default: "east"}
	if _, err := c.Resolve(p, ""); err == nil {
		t.Fatal("removed default fell back")
	}
	if id, err := c.Resolve(p, "west"); err != nil || id != "west" {
		t.Fatal("unrelated removal blocked valid choice", id, err)
	}
}
