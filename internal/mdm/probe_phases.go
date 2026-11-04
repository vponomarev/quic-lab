package mdm

import (
	"encoding/json"
	"time"
)

const ProbePhasesCapability = "vless_probe_phases_v2"
const ProbeSequenceCapability = "vless_probe_sequence_v2"
const ProbeSequenceKind = "vless_probe_sequence"

func probeSequenceAllowed(b bindingState) bool {
	if !probeAllowed(b) || !probePhasesAllowed(b) {
		return false
	}
	for _, c := range b.Report.Capabilities {
		if c == ProbeSequenceCapability {
			return true
		}
	}
	return false
}

type ProbePhase struct {
	Network    string     `json:"network"`
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	Rows       []ProbeRow `json:"rows"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

func probePhasesAllowed(b bindingState) bool {
	if b.Report != nil {
		for _, c := range b.Report.Capabilities {
			if c == ProbePhasesCapability {
				return true
			}
		}
	}
	return false
}

func validProbe(p ProbeSnapshot) bool {
	if p.Schema == 0 || p.Schema == 1 {
		return len(p.Phases) == 0 && validLegacyProbe(p)
	}
	if p.Schema != 2 || len(p.Rows) != 0 || len(p.Phases) < 1 || len(p.Phases) > 2 {
		return false
	}
	raw, e := json.Marshal(p)
	if e != nil || len(raw) > 768*1024 {
		return false
	}
	header := p
	header.Phases = nil
	header.Schema = 0
	if !validLegacyProbe(header) {
		return false
	}
	if p.Network != "none" {
		if _, ok := p.Phases[p.Network]; !ok {
			return false
		}
	}
	var ids map[string]bool
	for kind, phase := range p.Phases {
		if !oneOf(kind, "wifi", "cell") || phase.Network != kind || len(phase.Rows) < 1 || len(phase.Rows) > 256 {
			return false
		}
		rows := append([]ProbeRow(nil), phase.Rows...)
		if phase.State == "skipped" {
			if !oneOf(phase.Reason, "no_network", "permission_denied", "budget") {
				return false
			}
			for i := range rows {
				if rows[i].Outcome != phase.Reason {
					return false
				}
				rows[i].Outcome = "queued"
			}
		} else if phase.Reason != "" {
			return false
		}
		for i := range rows {
			if rows[i].Outcome == "permission_denied" {
				rows[i].Outcome = "interrupted"
			}
		}
		sub := header
		sub.Network = kind
		sub.State = phase.State
		sub.Rows = rows
		if phase.State == "skipped" {
			sub.State = "finished"
		}
		if phase.State == "budget" {
			sub.State = "interrupted"
		}
		if !validLegacyProbe(sub) {
			return false
		}
		current := map[string]bool{}
		for _, r := range rows {
			current[r.ID] = true
		}
		if ids != nil {
			if len(ids) != len(current) {
				return false
			}
			for id := range ids {
				if !current[id] {
					return false
				}
			}
		}
		ids = current
	}
	return true
}
