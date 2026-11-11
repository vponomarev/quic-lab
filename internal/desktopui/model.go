//go:build linux

// Package desktopui is the unprivileged presentation layer. Systemd and the
// authenticated controller own the VPN; closing this UI never stops it.
package desktopui

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"quiclab/internal/desktopcontrol"
)

func humanBytes(value float64) string {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}
	units := []string{"Б", "КиБ", "МиБ", "ГиБ", "ТиБ"}
	for i, unit := range units {
		if value < 1024 || i == len(units)-1 {
			if i == 0 {
				return fmt.Sprintf("%.0f %s", value, unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return "0 Б"
}

type trafficRate struct {
	invocation string
	tx, rx     uint64
	at         float64
	valid      bool
}

func (t *trafficRate) update(invocation string, tx, rx uint64, now float64) (up, down float64) {
	old := *t
	*t = trafficRate{invocation, tx, rx, now, true}
	if !old.valid || invocation == "" || old.invocation != invocation || now <= old.at || tx < old.tx || rx < old.rx {
		return 0, 0
	}
	return float64(tx-old.tx) / (now - old.at), float64(rx-old.rx) / (now - old.at)
}

func transportState(s desktopcontrol.Status, now float64) string {
	if s.TransportState == "ready" || s.TransportState == "checking" {
		if s.ProbeAt == nil || math.IsNaN(*s.ProbeAt) || math.IsInf(*s.ProbeAt, 0) ||
			(s.ProbeIntervalMS != 1000 && s.ProbeIntervalMS != 5000) {
			return "stale"
		}
		age := now - *s.ProbeAt
		if math.IsNaN(age) || age < 0 || age > math.Max(3*float64(s.ProbeIntervalMS)/1000, 5) {
			return "stale"
		}
	}
	return s.TransportState
}
func latency(s desktopcontrol.Status, now float64) string {
	if s.Active != "active" || s.SessionState == "reconnecting" || s.Readiness == "unavailable" || s.Readiness == "degraded" || transportState(s, now) != "ready" || s.RTTMS == nil || math.IsNaN(*s.RTTMS) || math.IsInf(*s.RTTMS, 0) || *s.RTTMS < 0 || *s.RTTMS > 60000 {
		return "RTT: —"
	}
	kind := map[string]string{"echo": "echo внутри туннеля", "icmp": "ICMP через AWG", "https": "HTTPS через VLESS"}[s.Probe]
	if kind == "" {
		kind = "проверка туннеля"
	}
	return fmt.Sprintf("RTT: %.1f мс · %s", *s.RTTMS, kind)
}
func describe(s desktopcontrol.Status, now float64) (string, string) {
	switch s.Active {
	case "failed":
		return "Ошибка подключения", "Проверьте профиль и доступность сервера."
	case "inactive":
		return "VPN отключён", "Трафик использует обычные системные маршруты."
	case "activating", "reloading":
		return "Подключение…", "Служба запускается."
	case "deactivating":
		return "Отключение…", "Служба освобождает маршруты и сетевые настройки."
	}
	if s.SessionState == "reconnecting" {
		return "Восстановление VPN…", "Служба повторяет подключение к серверу."
	}
	if s.Readiness == "unavailable" {
		return "VPN недоступен", "Последняя проверка DNS через VPN завершилась ошибкой."
	}
	if s.Readiness == "degraded" {
		return "Проверка VPN не прошла", "Последняя проверка DNS через VPN завершилась ошибкой."
	}
	switch transportState(s, now) {
	case "unavailable":
		return "Туннель не отвечает", "Проверка транспорта не прошла. Ожидаем следующего ответа."
	case "stale":
		return "Нет свежих ответов VPN", "Последнее измерение устарело. Проверяем соединение."
	case "checking":
		return "Проверка VPN…", "Транспорт запущен. Ожидаем ответа через туннель."
	case "ready":
		dns := "Проверка DNS через VPN ещё не подтверждена."
		if s.Readiness == "vpn_dns_ready" {
			dns = "DNS через VPN проверен."
		}
		return "VPN подключён", "Туннель отвечает. " + dns
	}
	switch s.Readiness {
	case "vpn_dns_ready":
		return "VPN запущен", "Проверка DNS через VPN успешна."
	case "engine_ready":
		return "VPN запущен", "Транспорт запущен. Доступность интернета не проверялась."
	}
	return "Служба работает", "Готовность VPN не подтверждена текущим журналом."
}

var eventLabels = map[string]string{
	"running": "VPN запущен", "stopped": "VPN остановлен",
	"ready": "Соединение восстановлено", "reconnecting": "Восстановление соединения",
	"engine_ready": "Транспорт запущен", "vpn_dns_ready": "DNS через VPN проверен",
	"checking": "Проверка соединения", "unavailable": "Соединение недоступно",
	"degraded": "Проверка DNS через VPN не прошла", "flow_failed": "Не удалось открыть одно из соединений",
	"reload_applied": "Правила применены", "reload_rejected": "Обновление правил отклонено",
}

// state is confined to the Fyne event goroutine. A status request leaves controls
// unchanged. One user action can wait behind it; its stale snapshot is discarded.
type state struct {
	status                                       *desktopcontrol.Status
	choice                                       string
	inflight, busy, changing, confirming, closed bool
	operation                                    string
	pending                                      *desktopcontrol.Command
	rate                                         trafficRate
	traffic, speed, events, errText              string
}
type controlState struct {
	Toggle                                           string
	CanToggle, CanReconnect, CanChoose, CanAutostart bool
}

func (s *state) knownCurrent() bool {
	if s.status != nil {
		for _, p := range s.status.Profiles {
			if p.ID == s.status.Selected {
				return true
			}
		}
	}
	return false
}
func (s *state) controls() controlState {
	c := controlState{Toggle: "Подключить"}
	if s.status == nil {
		return c
	}
	active := s.status.Active == "active"
	if active {
		c.Toggle = "Переключить"
		if s.choice == s.status.Selected || !s.knownCurrent() {
			c.Toggle = "Отключить"
		}
	}
	ready := !s.busy && !s.confirming && !s.closed
	transitional := s.status.Active == "activating" || s.status.Active == "deactivating" || s.status.Active == "reloading"
	c.CanToggle = ready && !transitional && (s.choice != "" || active)
	c.CanChoose = ready && !transitional
	c.CanReconnect = ready && !transitional && active && s.knownCurrent()
	c.CanAutostart = ready
	return c
}
func (s *state) connectionCommand() (desktopcontrol.Command, bool) {
	if !s.controls().CanToggle {
		return desktopcontrol.Command{}, false
	}
	if s.status.Active == "active" && (s.choice == s.status.Selected || !s.knownCurrent()) {
		return desktopcontrol.Command{Op: "disconnect"}, false
	}
	return desktopcontrol.Command{Op: "connect", Profile: s.choice}, s.status.Active == "active"
}
func (s *state) begin(command desktopcontrol.Command) bool {
	if s.closed || s.busy || s.confirming {
		return false
	}
	if command.Op == "status" {
		if s.inflight {
			return false
		}
	} else {
		if s.status == nil {
			return false
		}
		s.busy = true
		s.changing = command.Op == "connect" || command.Op == "disconnect" || command.Op == "reconnect"
		s.errText = ""
		if s.inflight {
			s.pending = &command
			return false
		}
	}
	s.operation = command.Op
	s.inflight = true
	return true
}
func (s *state) finish(result desktopcontrol.Status, err error, now float64) *desktopcontrol.Command {
	if s.closed {
		return nil
	}
	if s.pending != nil {
		next := s.pending
		s.pending = nil
		s.operation = next.Op
		// Keep the busy state and optimistic widgets. Neither the older snapshot nor
		// its failure may undo an action the user has already requested.
		return next
	}
	op := s.operation
	s.inflight = false
	s.busy = false
	s.changing = false
	if err != nil {
		s.errText = desktopcontrol.ErrorText(err)
		if op == "status" || op == "connect" || op == "disconnect" || op == "reconnect" {
			s.status = nil
			s.rate = trafficRate{}
			s.traffic = "Получено: —    Отправлено: —"
			s.speed = "↓ — /с    ↑ — /с"
			s.events = "События текущего подключения недоступны."
		}
		return nil
	}
	previous := ""
	if s.status != nil {
		previous = s.status.Selected
	}
	s.status = &result
	if s.choice == "" || s.choice == previous {
		s.choice = result.Selected
	}
	found := false
	for _, p := range result.Profiles {
		if p.ID == s.choice {
			found = true
		}
	}
	if !found {
		s.choice = ""
		if len(result.Profiles) > 0 {
			s.choice = result.Profiles[0].ID
		}
	}
	up, down := s.rate.update(result.Invocation, result.TXBytes, result.RXBytes, now)
	s.traffic = "Получено: " + humanBytes(float64(result.RXBytes)) + "    Отправлено: " + humanBytes(float64(result.TXBytes))
	s.speed = "↓ " + humanBytes(down) + "/с    ↑ " + humanBytes(up) + "/с"
	events := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		if text, ok := eventLabels[event]; ok {
			events = append(events, text)
		}
	}
	s.events = strings.Join(events, "\n")
	if s.events == "" {
		s.events = "Нет новых событий в доступной части журнала."
	}
	// Automatic polls must not erase an action failure before it can be read.
	// Unknown client errors map to the controller's safe connectivity message.
	if op != "status" || s.errText == desktopcontrol.ErrorText(errors.New("service_unavailable")) {
		s.errText = ""
	}
	return nil
}
func (s *state) health(now float64) (title, detail, rtt string) {
	rtt = "RTT: —"
	if s.status == nil {
		if s.errText != "" {
			return "Состояние VPN неизвестно", "Проверьте локальную службу управления. VPN мог продолжить работу.", rtt
		}
		return "Проверка состояния…", "Подключение к локальной службе управления.", rtt
	}
	title, detail = describe(*s.status, now)
	if s.changing {
		title = "Подключение…"
		op := s.operation
		if s.pending != nil {
			op = s.pending.Op
		}
		if op == "disconnect" {
			title = "Отключение…"
		}
	} else {
		rtt = latency(*s.status, now)
	}
	if s.busy {
		detail = "Применение команды…"
	}
	return
}
func (s *state) close() { s.closed = true; s.pending = nil }
