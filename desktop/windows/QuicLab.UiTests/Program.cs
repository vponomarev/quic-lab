using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Text.Json;
using QuicLab.Windows;
class UiTests
{
    [STAThread] static int Main(string[] args) { int exit = 1; var app = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown }; app.Startup += async (_, _) => { try { var client = new PipeClient(); var w = new MainWindow(client); w.Show(); await Ready(w); using var doc = JsonDocument.Parse(await File.ReadAllTextAsync(args[0])); ((TextBox)w.FindName("ConfigText")).Text = doc.RootElement.GetProperty("config").GetRawText(); await Click(w, "ValidateButton"); await Click(w, "ApplyButton"); await Click(w, "StartButton"); if (!Text(w, "StateText").Contains("запущено")) throw new Exception("start state"); w.Close(); var w2 = new MainWindow(client); w2.Show(); await Ready(w2); if (!Text(w2, "StateText").Contains("запущено")) throw new Exception("window close stopped service"); await Click(w2, "StopButton"); if (!Text(w2, "StateText").Contains("остановлено")) throw new Exception("stop state"); w2.Close(); Console.WriteLine("WPF event binding + close/reopen + SCM IPC PASS"); exit = 0; } catch (Exception e) { Console.WriteLine(e.GetType().Name + ": " + e.Message); } finally { app.Shutdown(); } }; app.Run(); return exit; }
    static string Text(MainWindow w, string name) => ((TextBlock)w.FindName(name)).Text;
    static async Task Ready(MainWindow w) { var until = DateTime.UtcNow.AddSeconds(8); while (DateTime.UtcNow < until) { if (Text(w, "MessageText").StartsWith("Готово")) return; if (Text(w, "MessageText").StartsWith("Не выполнено") || Text(w, "MessageText").StartsWith("Служба недоступна")) throw new Exception(Text(w, "MessageText")); await Task.Delay(25); } throw new TimeoutException("UI operation"); }
    static async Task Click(MainWindow w, string name) { var b = (Button)w.FindName(name); if (!b.IsEnabled) throw new Exception("button disabled"); b.RaiseEvent(new RoutedEventArgs(Button.ClickEvent)); await Ready(w); }
}
