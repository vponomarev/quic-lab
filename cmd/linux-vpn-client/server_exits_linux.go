//go:build linux && !android

package main

import (
	"context"
	"encoding/json"
	"io"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/linuxclient"
)

func serverExitCommand(ctx context.Context, cfg linuxclient.Config, choice, revision string, out io.Writer, client exitcontrol.Client) error {
	if cfg.ServerControl == nil {
		return exitcontrol.ErrUnsupported
	}
	view, err := client.Fetch(ctx, *cfg.ServerControl)
	if err != nil {
		return err
	}
	if choice != "" {
		id := choice
		if id == "default" {
			id = ""
		}
		if revision == "" {
			revision = view.Revision
		}
		view, err = client.Select(ctx, *cfg.ServerControl, exitcontrol.Change{ID: id, Revision: revision})
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(view)
}
