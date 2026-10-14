using System.IO;
using System.Text;
using System.Text.Json;
using System.Windows;
using System.Windows.Media;
using System.Windows.Threading;
using Microsoft.Win32;
namespace QuicLab.Windows;

public partial class MainWindow : Window
{
    readonly IStatusClient client;
    readonly CancellationTokenSource lifetime = new();
    readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromSeconds(1) };
    bool busy, closed, connectedToService, startPending, cancelPending, cancelRequested;
    Reply? latest;
    TaskCompletionSource<bool>? startSettled;
    public MainWindow(IStatusClient client)
    {
        InitializeComponent(); this.client = client;
        Loaded += async (_, _) => { await Command("status"); if (!closed) timer.Start(); };
        timer.Tick += async (_, _) => { if (connectedToService && latest is not null) RenderTraffic(latest); await Command("status", true); };
        Closed += (_, _) => { closed = true; timer.Stop(); lifetime.Cancel(); };
    }
    void SetBusy(bool value, bool polling = false) { busy = value; BusyBar.Visibility = value && !polling ? Visibility.Visible : Visibility.Collapsed; UpdateButtons(); }
    void UpdateButtons()
    {
        ToggleButton.IsEnabled = !cancelPending && connectedToService && (startPending ? !cancelRequested : !busy && latest?.State is "stopped" or "running" or "failed" or "starting");
        if (cancelPending) ToggleButton.Content = "Отмена…";
        else if (startPending || latest?.State == "starting") ToggleButton.Content = "Отменить";
        else ToggleButton.Content = latest?.State == "running" ? "Отключить" : latest?.State == "failed" ? "Сбросить ошибку" : "Подключить";
        RefreshButton.IsEnabled = ImportButton.IsEnabled = !busy && !cancelPending;
        ValidateButton.IsEnabled = !busy && !cancelPending && connectedToService && latest?.Runtime?.Mode == "fixture";
        ApplyButton.IsEnabled = ValidateButton.IsEnabled && latest?.State == "stopped";
    }
    void Render(Reply r)
    {
        latest = r; connectedToService = true; bool running = r.State == "running", fixture = r.Runtime?.Mode == "fixture", vpn = r.Runtime?.Mode == "vpn";
        StateText.Text = r.State switch { "running" => fixture ? "Тест запущен" : vpn ? "Движок запущен" : "VPN не подтверждён", "stopped" => "Отключено", "starting" => "Подключение…", "stopping" => "Отключение…", _ => "Ошибка подключения" };
        StatusDot.Fill = new SolidColorBrush(running ? Color.FromRgb(0, 135, 127) : r.State == "failed" ? Color.FromRgb(183, 71, 64) : Color.FromRgb(139, 156, 165));
        ToggleButton.Content = running ? "Отключить" : r.State == "failed" ? "Сбросить ошибку" : "Подключить";
        ProfileText.Text = r.Runtime?.Mode == "vpn" ? $"{r.Runtime.Transport?.ToUpperInvariant()} · {r.Runtime.Endpoint}\nПодсетей через VPN: {r.Runtime.RouteCount}" : fixture ? "Проверка интерфейса · без VPN" : "Профиль службы";
        ModeBanner.Visibility = Visibility.Visible; ModeText.Text = vpn ? "IPv4: " + (r.Runtime?.Targets is { Length: >0 } targets ? string.Join(", ",targets) : "список подсетей служба не сообщает") + "\nОстальной трафик и системный DNS не перенастраиваются. В preview нет kill switch: после аварии службы выбранный трафик может пойти напрямую." : fixture ? "Тестовый движок: сеть не изменяется, трафик не передаётся." : "Служба не сообщает тип движка. Работа VPN не подтверждена.";
        ConfigText.ToolTip = vpn ? "Профиль MVP задаётся администратором в engine.json. Импорт через окно доступен только тестовому движку." : null;
        RenderTraffic(r); UpdateButtons();
    }
    void RenderTraffic(Reply r)
    {
        var t = TrafficDisplay.Create(r, DateTimeOffset.UtcNow); TxTotalText.Text = t.TxTotal; RxTotalText.Text = t.RxTotal; TxRateText.Text = t.TxRate; RxRateText.Text = t.RxRate; TrafficHintText.Text = t.Hint;
    }
    async Task Command(string command, bool polling = false)
    {
        if (busy || cancelPending || closed) return;
        if (command == "start") { startPending = true; cancelRequested = false; startSettled = new(TaskCreationOptions.RunContinuationsAsynchronously); StateText.Text = "Подключение…"; }
        bool authenticatedReply = false;
        SetBusy(true, polling); if (!polling) MessageText.Text = "Выполняется…";
        try
        {
            JsonElement? config = null; if (command is "validate_config" or "apply_config") { if (Encoding.UTF8.GetByteCount(ConfigText.Text) > Frames.Maximum) throw new InvalidDataException(); using var doc = JsonDocument.Parse(ConfigText.Text); config = doc.RootElement.Clone(); }
            var reply = await client.SendAsync(command, config, lifetime.Token); authenticatedReply = true; if (closed || command == "start" && cancelRequested) return; Render(reply);
            if (!reply.Ok) MessageText.Text = ErrorText(reply.Error); else if (!polling) MessageText.Text = $"Готово · ревизия {reply.Revision}";
        }
        catch (OperationCanceledException) when (closed) { }
        catch (JsonException) { MessageText.Text = "Проверьте формат JSON."; }
        catch (UnauthorizedAccessException) { if (!(command == "start" && cancelRequested)) Unavailable("Не удалось подтвердить подлинность службы. Данные не отправлены."); }
        catch (Exception) { if (!closed && !(command == "start" && cancelRequested)) Unavailable("Служба недоступна. Для настоящего VPN требуется установка службы; окно не запрашивает UAC автоматически."); }
        finally { if (command == "start") { startPending = false; startSettled?.TrySetResult(authenticatedReply); } if (!closed) SetBusy(false); }
    }
    void Unavailable(string message) { connectedToService = false; StateText.Text = "Нет связи со службой"; StatusDot.Fill = Brushes.Gray; MessageText.Text = message; TxTotalText.Text = RxTotalText.Text = TxRateText.Text = RxRateText.Text = "—"; TrafficHintText.Text = "Нет актуальных данных"; UpdateButtons(); }
    static string ErrorText(string code) => code switch { "config_required" => "Сначала откройте и примените конфигурацию.", "stop_before_apply" => "Отключите VPN перед применением настроек.", "invalid_config" => "Конфигурация не поддерживается или содержит ошибку.", "cleanup_required" => "Не удалось завершить очистку. Повторите отключение.", "backend_start_failed" => "Подключение не удалось. Проверьте профиль и сеть.", _ => "Команда не выполнена. Обновите статус и повторите." };
    async void ToggleClick(object sender, RoutedEventArgs e)
    {
        if (startPending) { await CancelStart(); return; }
        await Command(latest?.State is "running" or "failed" or "starting" ? "stop" : "start");
    }
    async Task CancelStart()
    {
        if (cancelPending || cancelRequested || closed) return;
        cancelPending = cancelRequested = true; StateText.Text = "Отмена подключения…"; MessageText.Text = "Выполняется…"; UpdateButtons();
        try {
            // A separate authenticated connection can stop an in-flight Start.
            var settlement = startSettled ?? throw new InvalidOperationException();
            try { await client.SendAsync("stop", null, lifetime.Token); }
            catch (Exception) when (!closed) { /* Retry after the original request settles. */ }
            bool startConfirmed = await settlement.Task.WaitAsync(lifetime.Token);
            // Stop can overtake Start before the service accepts it. Fence that order.
            var reply = await client.SendAsync("stop", null, lifetime.Token);
            if (closed) return; Render(reply);
            MessageText.Text = !startConfirmed ? "Результат запуска неизвестен. Команда остановки отправлена; следите за статусом службы." : reply.Ok ? "Готово · подключение отменено" : ErrorText(reply.Error);
        }
        catch (OperationCanceledException) when (closed) { }
        catch (Exception) { if (!closed) Unavailable("Не удалось подтвердить остановку. Обновите статус службы."); }
        finally { cancelPending = false; if (!closed) UpdateButtons(); }
    }
    void ApplicationsClick(object sender, RoutedEventArgs e) => new ApplicationSelectionWindow { Owner = this }.ShowDialog();
    async void RefreshClick(object sender, RoutedEventArgs e) => await Command("status");
    async void ValidateClick(object sender, RoutedEventArgs e) => await Command("validate_config");
    async void ApplyClick(object sender, RoutedEventArgs e) => await Command("apply_config");
    async void ImportClick(object sender, RoutedEventArgs e)
    {
        if (busy) return; var dialog = new OpenFileDialog { Filter = "JSON|*.json" }; if (dialog.ShowDialog() != true) return; SetBusy(true);
        try { await using var file = new FileStream(dialog.FileName, FileMode.Open, FileAccess.Read, FileShare.Read, 4096, true); using var limited = new MemoryStream(); byte[] buffer = new byte[8192]; int n; while ((n = await file.ReadAsync(buffer, lifetime.Token)) != 0) { if (limited.Length + n > Frames.Maximum) throw new InvalidDataException(); await limited.WriteAsync(buffer.AsMemory(0, n), lifetime.Token); } if (closed) return; ConfigText.Text = new UTF8Encoding(false, true).GetString(limited.ToArray()).TrimStart('\uFEFF'); MessageText.Text = "Файл открыт. Проверьте и примените настройки."; }
        catch (Exception) { if (!closed) MessageText.Text = "Не удалось открыть конфигурацию (не UTF-8 или больше 1 МиБ)."; }
        finally { if (!closed) SetBusy(false); }
    }
}
