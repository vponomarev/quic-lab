using System.IO;
using System.Text.Json;
using System.Windows;
using Microsoft.Win32;
namespace QuicLab.Windows;

public partial class MainWindow : Window
{
    readonly PipeClient client; bool busy;
    public MainWindow(PipeClient client) { InitializeComponent(); this.client = client; Loaded += async (_, _) => await Command("status"); }
    void SetBusy(bool value) { busy = value; StartButton.IsEnabled = StopButton.IsEnabled = RefreshButton.IsEnabled = ImportButton.IsEnabled = ValidateButton.IsEnabled = ApplyButton.IsEnabled = !value; }
    async Task Command(string command) { if (busy) return; SetBusy(true); MessageText.Text = "Выполняется…"; try { JsonElement? config = null; if (command is "validate_config" or "apply_config") { if (System.Text.Encoding.UTF8.GetByteCount(ConfigText.Text) > Frames.Maximum) throw new InvalidDataException(); using var doc = JsonDocument.Parse(ConfigText.Text); config = doc.RootElement.Clone(); } var reply = await client.SendAsync(command, config); StateText.Text = reply.State switch { "running" => "Тестовая служба: запущено", "stopped" => "Тестовая служба: остановлено", "starting" => "Запуск…", "stopping" => "Остановка…", _ => "Требуется внимание" }; MessageText.Text = reply.Ok ? $"Готово · ревизия {reply.Revision}" : $"Не выполнено: {reply.Error}"; } catch (JsonException) { MessageText.Text = "Проверьте формат JSON."; } catch (UnauthorizedAccessException) { MessageText.Text = "Не удалось подтвердить подлинность службы. Данные не отправлены."; } catch (Exception) { MessageText.Text = "Служба недоступна или запрос не выполнен. Проверьте её запуск и повторите."; } finally { SetBusy(false); } }
    async void StartClick(object sender, RoutedEventArgs e) => await Command("start"); async void StopClick(object sender, RoutedEventArgs e) => await Command("stop"); async void RefreshClick(object sender, RoutedEventArgs e) => await Command("status"); async void ValidateClick(object sender, RoutedEventArgs e) => await Command("validate_config"); async void ApplyClick(object sender, RoutedEventArgs e) => await Command("apply_config");
    async void ImportClick(object sender, RoutedEventArgs e) { var dialog = new OpenFileDialog { Filter = "JSON|*.json" }; if (dialog.ShowDialog() != true) return; try { if (new FileInfo(dialog.FileName).Length > Frames.Maximum) throw new InvalidDataException(); ConfigText.Text = await File.ReadAllTextAsync(dialog.FileName); MessageText.Text = "Файл открыт. Проверьте конфигурацию и нажмите «Применить»."; } catch (Exception) { MessageText.Text = "Не удалось открыть конфигурацию."; } }
}
