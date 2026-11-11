// Package desktopcontrol implements the bounded, local desktop VPN control protocol.
package desktopcontrol

import "errors"

const DefaultSocket = "/run/quic-lab-control/control.sock"

type Command struct {
	Op      string `json:"op"`
	Profile string `json:"profile,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type Profile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Status struct {
	Selected        string    `json:"selected"`
	Profiles        []Profile `json:"profiles"`
	Active          string    `json:"active"`
	Autostart       bool      `json:"autostart"`
	Invocation      string    `json:"invocation"`
	Failed          bool      `json:"failed"`
	Started         bool      `json:"started"`
	SessionState    string    `json:"session_state"`
	Readiness       string    `json:"readiness"`
	Events          []string  `json:"events"`
	TXBytes         uint64    `json:"tx_bytes"`
	RXBytes         uint64    `json:"rx_bytes"`
	TransportState  string    `json:"transport_state"`
	RTTMS           *float64  `json:"rtt_ms"`
	ProbeAt         *float64  `json:"probe_at"`
	ProbeIntervalMS int       `json:"probe_interval_ms"`
	Probe           string    `json:"probe"`
}

type controlError string

func (e controlError) Error() string { return string(e) }

var errorTexts = map[controlError]string{
	"access_denied":             "Этому пользователю не разрешено управление VPN.",
	"unsafe_configuration":      "Профиль или права его файла требуют проверки администратора.",
	"invalid_request":           "Команда не поддерживается установленной службой.",
	"operation_failed":          "Служба не смогла выполнить команду. Проверьте её состояние.",
	"response_too_large":        "Служба вернула слишком большой ответ.",
	"service_unavailable":       "Нет связи со службой управления. Состояние VPN неизвестно.",
	"service_busy":              "Служба выполняет другую операцию. Попробуйте через несколько секунд.",
	"unmanaged_profile":         "Активный профиль настроен вручную. GUI не будет заменять его.",
	"unmanaged_service":         "Служба VPN использует другую команду запуска. Требуется настройка администратором.",
	"start_not_ready":           "VPN не подтвердил запуск за отведённое время.",
	"switch_failed_rolled_back": "Подключение не удалось. Прежний профиль и его состояние восстановлены.",
	"rollback_failed":           "Подключение и восстановление не удались. Требуется проверка службы.",
	"untrusted_server":          "Не удалось подтвердить подлинность службы управления.",
	"invalid_response":          "Несовместимый ответ службы управления.",
}

// ErrorText returns only a known, user-safe localized message, never raw errors.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return errorTexts[safeError(err)]
}

func safeError(err error) controlError {
	var code controlError
	if errors.As(err, &code) {
		if _, ok := errorTexts[code]; ok {
			return code
		}
	}
	return "service_unavailable"
}

var profiles = []Profile{{"quic", "QUIC"}, {"https", "HTTPS"}, {"awg", "AmneziaWG"}, {"vless", "VLESS"}}

func knownProfile(name string) bool {
	for _, p := range profiles {
		if p.ID == name {
			return true
		}
	}
	return false
}

func validInvocation(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
