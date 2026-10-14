using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;
partial class UiTests
{
    [STAThread]
    static int Main(string[] args)
    {
        int exit = 1; var app = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown }; app.Startup += async (_, _) =>
        {
            try
            {
                if (args.Length == 2 && args[0] == "--live") { await LiveAcceptance(args[1]); exit = 0; return; }
                await ServiceRecoveryAcceptance();
                await CompactLayoutAcceptance();
                await ApplicationSelectionAcceptance();
                var fake = new FakeClient(); var w = new MainWindow(fake); w.Show(); await Ready(w);
                Check(w.Width <= 460, "compact window"); Check(!((Expander)w.FindName("SettingsPanel")).IsExpanded, "settings initially collapsed");
                await Click(w, "ToggleButton"); Check(Text(w, "StateText") == "Движок запущен", "start state"); Check(Text(w, "TxTotalText") == "2 КиБ", "TX total"); Check(Text(w, "RxRateText") == "2 КиБ/с", "RX rate");
                var preview = Environment.GetEnvironmentVariable("QUIC_UI_PREVIEW");
                if (!string.IsNullOrEmpty(preview)) { w.UpdateLayout(); var bitmap = new System.Windows.Media.Imaging.RenderTargetBitmap((int)w.ActualWidth, (int)w.ActualHeight, 96, 96, System.Windows.Media.PixelFormats.Pbgra32); bitmap.Render(w); var encoder = new System.Windows.Media.Imaging.PngBitmapEncoder(); encoder.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bitmap)); using var image = System.IO.File.Create(preview); encoder.Save(image); }
                int polls = fake.Calls; await Task.Delay(1300); Check(fake.Calls > polls, "automatic polling");
                fake.DelayStatus = true; await Task.Delay(5500); Check(Text(w, "TxRateText") == "—", "delayed polling must age rates"); fake.DelayStatus = false;
                w.Close(); int closedCalls = fake.Calls; await Task.Delay(1300); Check(fake.Calls == closedCalls, "polling stopped on close"); Check(fake.State == "running", "closing UI stopped backend");
                var w2 = new MainWindow(fake); w2.Show(); await Ready(w2); Check(Text(w2, "StateText") == "Движок запущен", "reopen state"); await Click(w2, "ToggleButton"); Check(Text(w2, "TxRateText") == "0 Б/с", "stopped rate");
                fake.Fail = true; await Click(w2, "RefreshButton", false); Check(!((Button)w2.FindName("ToggleButton")).IsEnabled, "unavailable must disable start"); Check(Text(w2, "TxRateText") == "—", "unavailable stats"); w2.Close();
                var legacy = new MainWindow(new FakeClient { State = "running", Legacy = true }); legacy.Show(); await Ready(legacy); Check(((Border)legacy.FindName("ModeBanner")).Visibility == Visibility.Visible, "legacy service warning"); Check(Text(legacy, "StateText") != "Движок запущен", "legacy cannot confirm VPN"); legacy.Close();
                var delayed = new DelayedStartClient(); var cancelWindow = new MainWindow(delayed); cancelWindow.Show(); await Ready(cancelWindow);
                await Click(cancelWindow, "ToggleButton", false); Check(Text(cancelWindow,"StateText")=="Подключение…","pending start state"); Check(((Button)cancelWindow.FindName("ToggleButton")).Content.ToString()=="Отменить","cancel label");
                await Click(cancelWindow,"ToggleButton"); Check(delayed.Stops==2,"cancel must fence a late accepted start"); Check(Text(cancelWindow,"StateText")=="Отключено","stop state"); await Task.Delay(100); Check(Text(cancelWindow,"StateText")=="Отключено","late start must not overwrite stop"); cancelWindow.Close();
                var uncertain = new DelayedStartClient { FailStart = true }; var unknown = new MainWindow(uncertain); unknown.Show(); await Ready(unknown); await Click(unknown,"ToggleButton",false); await Click(unknown,"ToggleButton",false); await Task.Delay(250); Check(Text(unknown,"MessageText").Contains("неизвестен"),"lost start reply cannot confirm cancellation"); unknown.Close();
                if (args.Length == 3) { var client = new PipeClient(args[1], uint.Parse(args[2])); var native = new MainWindow(client); native.Show(); await Ready(native); using var doc = JsonDocument.Parse(await System.IO.File.ReadAllTextAsync(args[0])); ((TextBox)native.FindName("ConfigText")).Text = doc.RootElement.GetProperty("config").GetRawText(); await Click(native, "ValidateButton"); await Click(native, "ApplyButton"); await Click(native, "ToggleButton"); Check(Text(native, "StateText") == "Тест запущен", "native fixture label"); Check(Text(native, "TxTotalText") == "—", "fixture cannot invent traffic"); await Click(native, "ToggleButton"); native.Close(); }
                Console.WriteLine("WPF dashboard: state/stats/polling/close/reopen/unavailable/native fixture PASS"); exit = 0;
            }
            catch (Exception e) { Console.WriteLine(e.GetType().Name + ": " + e.Message); }
            finally { app.Shutdown(); }
        }; app.Run(); return exit;
    }
    static void Check(bool b, string why) { if (!b) throw new Exception(why); }
    static string Text(MainWindow w, string n) => ((TextBlock)w.FindName(n)).Text;
    static async Task Ready(MainWindow w) { var until = DateTime.UtcNow.AddSeconds(8); while (DateTime.UtcNow < until) { if (Text(w, "MessageText").StartsWith("Готово")) return; await Task.Delay(25); } throw new TimeoutException(Text(w, "MessageText")); }
    static async Task Click(MainWindow w, string name, bool ready = true) { var b = (Button)w.FindName(name); var until = DateTime.UtcNow.AddSeconds(4); while (!b.IsEnabled && DateTime.UtcNow < until) await Task.Delay(25); Check(b.IsEnabled, "button disabled " + name); b.RaiseEvent(new RoutedEventArgs(Button.ClickEvent)); if (ready) await Ready(w); else await Task.Delay(100); }
    sealed class DelayedStartClient : IStatusClient
    {
        public TaskCompletionSource Finish = new(TaskCreationOptions.RunContinuationsAsynchronously); public int Stops; public bool FailStart; string state="stopped";
        public async Task<Reply> SendAsync(string command, JsonElement? config=null, CancellationToken cancellationToken=default) {
            if(command=="start") {state="starting";await Finish.Task.WaitAsync(cancellationToken);state="running";if(FailStart) throw new System.IO.IOException();return new Reply(true,"running","",1);}
            if(command=="stop") {Stops++;state="stopped"; if(Stops==1) _=Task.Run(async()=>{await Task.Delay(100);Finish.TrySetResult();});}
            return new Reply(true,state,"",1);
        }
    }
    sealed class FakeClient : IStatusClient
    {
        public string State = "stopped"; public int Calls; public bool Fail, DelayStatus, Legacy;
        public async Task<Reply> SendAsync(string command, JsonElement? config = null, CancellationToken cancellationToken = default) { Calls++; if (command == "status" && DelayStatus) await Task.Delay(15000, cancellationToken); if (Fail) throw new System.IO.IOException(); if (command == "start") State = "running"; if (command == "stop") State = "stopped"; return new Reply(true, State, "", 1, Legacy ? null : new RuntimeInfo("vpn", "quic", "192.0.2.1:443", 1, new TrafficInfo(true, "one", DateTimeOffset.UtcNow, 2048, 4096, 1024, 2048))); }
    }
}
