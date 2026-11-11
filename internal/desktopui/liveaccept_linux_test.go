//go:build linux && !android && guiacceptance

package desktopui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestLiveAcceptanceFindsNativeConfirmationButton(t *testing.T) {
	application := test.NewApp()
	defer application.Quit()
	window := application.NewWindow("native confirmation")
	defer window.Close()
	window.Resize(fyne.NewSize(620, 640))
	window.Show()
	confirmed := false
	confirmation := dialog.NewConfirm("Переключить VPN-подключение?", "Текущие соединения будут разорваны.", func(accepted bool) { confirmed = accepted }, window)
	confirmation.SetConfirmText("Переключить")
	confirmation.SetDismissText("Отмена")
	confirmation.Show()
	var button *widget.Button
	for _, overlay := range window.Canvas().Overlays().List() {
		if button = liveFindButton(overlay, "Переключить"); button != nil {
			break
		}
	}
	if button == nil {
		t.Fatal("native dialog confirmation button not found through overlay wrapper")
	}
	button.Tapped(&fyne.PointEvent{})
	if !confirmed {
		t.Fatal("native dialog callback did not accept")
	}
	if len(window.Canvas().Overlays().List()) != 0 {
		t.Fatal("native confirmation did not close")
	}
}
