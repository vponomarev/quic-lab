//go:build linux

package desktopui

import (
	"context"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"quiclab/internal/desktopcontrol"
)

// RequestFunc is the controller boundary. It must honor context cancellation.
type RequestFunc func(context.Context, desktopcontrol.Command) (desktopcontrol.Status, error)

// View owns the widgets and request lifecycle of one window. Its methods must
// be called on the Fyne event goroutine. Only request executes in a worker.
type View struct {
	window                                                        fyne.Window
	state                                                         state
	request                                                       RequestFunc
	dispatch                                                      func(func())
	now                                                           func() float64
	ctx                                                           context.Context
	cancel                                                        context.CancelFunc
	started                                                       bool
	updating                                                      bool
	profileIDs                                                    []string
	title, detail, current, rtt, traffic, speed, events, errLabel *widget.Label
	profiles                                                      *widget.Select
	toggle, reconnect, refresh                                    *widget.Button
	autostart                                                     *widget.Check
	confirm                                                       func(func(bool))
}

// NewView installs a view in window, without starting background work. Call
// Start once the window is ready. Passing nil request uses the local controller.
// Constructing a view does not initialize an application or graphics driver.
func NewView(window fyne.Window, request RequestFunc) *View {
	return newView(window, request, fyne.Do, desktopcontrol.Monotonic)
}
func newView(window fyne.Window, request RequestFunc, dispatch func(func()), now func() float64) *View {
	if request == nil {
		request = desktopcontrol.Call
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &View{window: window, request: request, dispatch: dispatch, now: now, ctx: ctx, cancel: cancel}
	v.title = widget.NewLabelWithStyle("Проверка состояния…", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	v.title.Wrapping = fyne.TextWrapWord
	v.detail = wrapped("Подключение к локальной службе управления.")
	v.current = wrapped("Текущий профиль: —")
	v.rtt = wrapped("RTT: —")
	v.traffic = wrapped("Получено: —    Отправлено: —")
	v.speed = wrapped("↓ — /с    ↑ — /с")
	v.events = wrapped("Нет новых событий в доступной части журнала.")
	v.errLabel = wrapped("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Hide()
	v.profiles = widget.NewSelect(nil, func(label string) {
		if v.updating {
			return
		}
		for i, text := range v.profiles.Options {
			if text == label && i < len(v.profileIDs) {
				v.state.choice = v.profileIDs[i]
				break
			}
		}
		v.renderControls()
	})
	v.profiles.PlaceHolder = "Выберите подключение"
	v.toggle = widget.NewButton("Подключить", v.changeConnection)
	v.toggle.Importance = widget.HighImportance
	v.reconnect = widget.NewButton("Переподключить", func() { v.execute(desktopcontrol.Command{Op: "reconnect"}) })
	v.autostart = widget.NewCheck("Подключать VPN при загрузке компьютера", func(enabled bool) {
		if !v.updating {
			v.execute(desktopcontrol.Command{Op: "autostart", Enabled: &enabled})
		}
	})
	v.refresh = widget.NewButtonWithIcon("Обновить", theme.ViewRefreshIcon(), v.poll)
	heading := container.NewBorder(nil, nil, container.NewVBox(widget.NewLabelWithStyle("QUIC Lab", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewLabel("Управление VPN")), v.refresh)
	statusCard := widget.NewCard("", "", container.NewVBox(v.title, v.detail, widget.NewSeparator(), v.current, v.rtt))
	profileRow := container.NewBorder(nil, nil, widget.NewLabel("Подключение"), nil, v.profiles)
	actions := container.NewGridWithColumns(2, v.toggle, v.reconnect)
	top := container.NewVBox(heading, statusCard, profileRow, actions, v.autostart, widget.NewSeparator(), v.traffic, v.speed, v.errLabel)
	footer := wrapped("Закрытие окна не отключает VPN. Изменение профиля разрывает текущие соединения.")
	footer.Importance = widget.LowImportance
	events := container.NewBorder(widget.NewLabelWithStyle("События текущего подключения", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, container.NewVScroll(v.events))
	window.SetContent(container.NewPadded(container.NewBorder(top, container.NewVBox(layout.NewSpacer(), footer), nil, nil, events)))
	window.SetOnClosed(v.Close)
	v.confirm = func(done func(bool)) {
		d := dialog.NewConfirm("Переключить VPN-подключение?", "Текущие соединения будут разорваны. Во время переключения обычный доступ к сети не блокируется.", done, window)
		d.SetConfirmText("Переключить")
		d.SetDismissText("Отмена")
		d.Show()
	}
	v.render()
	return v
}
func wrapped(text string) *widget.Label {
	w := widget.NewLabel(text)
	w.Wrapping = fyne.TextWrapWord
	return w
}

// Start begins two-second status polling and one-second local freshness checks.
func (v *View) Start() {
	if v.started || v.state.closed {
		return
	}
	v.started = true
	v.poll()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-v.ctx.Done():
				return
			case <-ticker.C:
				ticks++
				poll := ticks%2 == 0
				v.dispatch(func() {
					if v.state.closed {
						return
					}
					if poll {
						v.poll()
					} else {
						v.renderHealth()
					}
				})
			}
		}
	}()
}

// Close only cancels GUI work. It deliberately does not send disconnect or
// otherwise change the service, which can continue after the window is gone.
func (v *View) Close() { v.state.close(); v.cancel() }
func (v *View) poll() {
	if v.state.closed {
		return
	}
	v.renderHealth()
	v.execute(desktopcontrol.Command{Op: "status"})
}
func (v *View) execute(command desktopcontrol.Command) {
	if command.Op == "" {
		return
	}
	dispatch := v.state.begin(command)
	v.renderHealth()
	v.renderControls()
	v.renderError()
	if dispatch {
		v.send(command)
	}
}
func (v *View) send(command desktopcontrol.Command) {
	go func() {
		result, err := v.request(v.ctx, command)
		if v.ctx.Err() != nil {
			return
		}
		v.dispatch(func() {
			if v.state.closed {
				return
			}
			next := v.state.finish(result, err, v.now())
			if next != nil {
				// Preserve the user's checkbox/choice until this queued action completes.
				v.renderHealth()
				v.renderControls()
				v.send(*next)
				return
			}
			v.render()
		})
	}()
}
func (v *View) changeConnection() {
	command, confirm := v.state.connectionCommand()
	if command.Op == "" {
		return
	}
	if !confirm {
		v.execute(command)
		return
	}
	v.state.confirming = true
	v.renderControls()
	v.confirm(func(approved bool) {
		if v.state.closed {
			return
		}
		v.state.confirming = false
		if approved {
			v.execute(command)
		} else {
			v.renderControls()
		}
	})
}
func (v *View) render() {
	v.renderHealth()
	if s := v.state.status; s != nil {
		ids := make([]string, 0, len(s.Profiles))
		labels := make([]string, 0, len(s.Profiles))
		current := "настроен вручную"
		selected := ""
		for _, p := range s.Profiles {
			ids = append(ids, p.ID)
			labels = append(labels, p.Label)
			if p.ID == s.Selected {
				current = p.Label
			}
			if p.ID == v.state.choice {
				selected = p.Label
			}
		}
		v.updating = true
		if !slices.Equal(v.profileIDs, ids) || !slices.Equal(v.profiles.Options, labels) {
			v.profileIDs = ids
			v.profiles.SetOptions(labels)
		}
		if v.profiles.Selected != selected {
			v.profiles.SetSelected(selected)
		}
		if v.autostart.Checked != s.Autostart {
			v.autostart.SetChecked(s.Autostart)
		}
		v.updating = false
		setText(v.current, "Текущий профиль: "+current)
	}
	if v.state.traffic != "" {
		setText(v.traffic, v.state.traffic)
	}
	if v.state.speed != "" {
		setText(v.speed, v.state.speed)
	}
	if v.state.events != "" {
		setText(v.events, v.state.events)
	}
	v.renderError()
	v.renderControls()
}
func (v *View) renderHealth() {
	title, detail, rtt := v.state.health(v.now())
	setText(v.title, title)
	setText(v.detail, detail)
	setText(v.rtt, rtt)
}
func (v *View) renderError() {
	setText(v.errLabel, v.state.errText)
	if v.state.errText == "" {
		if v.errLabel.Visible() {
			v.errLabel.Hide()
		}
	} else if !v.errLabel.Visible() {
		v.errLabel.Show()
	}
}
func (v *View) renderControls() {
	c := v.state.controls()
	if v.toggle.Text != c.Toggle {
		v.toggle.SetText(c.Toggle)
	}
	enable(v.toggle, c.CanToggle)
	enable(v.reconnect, c.CanReconnect)
	enable(v.profiles, c.CanChoose)
	enable(v.autostart, c.CanAutostart)
	enable(v.refresh, !v.state.busy && !v.state.confirming && !v.state.closed)
}
func setText(label *widget.Label, text string) {
	if label.Text != text {
		label.SetText(text)
	}
}
func enable(w fyne.Disableable, enabled bool) {
	if enabled && w.Disabled() {
		w.Enable()
	} else if !enabled && !w.Disabled() {
		w.Disable()
	}
}
