using System.Net.Http;
using System.Net.Sockets;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

partial class UiTests
{
    // Explicit opt-in: requires the administrator-provisioned bounded 1.1.1.1/32 fixture.
    static async Task LiveAcceptance(string expectedExit)
    {
        if (!System.Net.IPAddress.TryParse(expectedExit, out var expected) || expected.AddressFamily != AddressFamily.InterNetwork) throw new ArgumentException("IPv4 exit required");
        var client = new PipeClient();
        var before = await client.SendAsync("status");
        Check(before.Ok && before.State == "stopped" && before.Runtime?.Mode == "vpn" && before.Runtime.RouteCount == 1, "bounded stopped real service required");
        MainWindow? window = null;
        try
        {
            window = new MainWindow(client); window.Show(); await Ready(window);
            await Click(window, "ToggleButton"); Check(Text(window,"StateText")=="Движок запущен", "real engine did not start");
            using (var route = new System.Diagnostics.Process { StartInfo = new System.Diagnostics.ProcessStartInfo {
                FileName = System.IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.System),"WindowsPowerShell","v1.0","powershell.exe"),
                Arguments = "-NoProfile -Command \"$r=Find-NetRoute -RemoteIPAddress 1.1.1.1; if(-not ($r | Where-Object InterfaceAlias -like 'QUIC Lab MVP *')){exit 1}\"",
                UseShellExecute=false,CreateNoWindow=true,RedirectStandardOutput=true,RedirectStandardError=true } }) {
                route.Start();using var deadline=new CancellationTokenSource(TimeSpan.FromSeconds(15));await route.WaitForExitAsync(deadline.Token);Check(route.ExitCode==0,"probe is not routed through owned adapter");
            }
            using var handler = new HttpClientHandler { UseProxy = false };
            using var http = new HttpClient(handler) { Timeout = TimeSpan.FromSeconds(15) };
            var trace = await http.GetStringAsync("https://1.1.1.1/cdn-cgi/trace");
            Check(trace.Split('\n').Any(line => line.Trim() == "ip="+expected), "unexpected VPN exit");
            byte[] query = Convert.FromHexString("123401000001000000000000076578616d706c6503636f6d0000010001");
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(10));
            using (var udp = new UdpClient(AddressFamily.InterNetwork)) {
                udp.Connect("1.1.1.1",53); await udp.SendAsync(query,timeout.Token); var reply = await udp.ReceiveAsync(timeout.Token);
                Check(reply.Buffer.Length > 12 && reply.Buffer[0]==0x12 && reply.Buffer[1]==0x34 && (reply.Buffer[2]&0x80)!=0,"UDP DNS response");
            }
            using (var tcp = new TcpClient(AddressFamily.InterNetwork)) {
                await tcp.ConnectAsync("1.1.1.1",53,timeout.Token); var stream=tcp.GetStream();
                byte[] length={0,(byte)query.Length}; await stream.WriteAsync(length,timeout.Token); await stream.WriteAsync(query,timeout.Token);
                await stream.ReadExactlyAsync(length,timeout.Token); int count=(length[0]<<8)|length[1]; Check(count>=12,"TCP DNS response size");
                var reply=new byte[count];await stream.ReadExactlyAsync(reply,timeout.Token);Check(reply[0]==0x12&&reply[1]==0x34&&(reply[2]&0x80)!=0,"TCP DNS response");
            }
            await Task.Delay(1800);
            var running=await client.SendAsync("status");
            Check(running.Runtime?.Traffic is {Available:true,TxBytes:>0,RxBytes:>0},"real counters missing");
            Check(Text(window,"TxTotalText")!="—"&&Text(window,"RxTotalText")!="—","UI counters missing");
            var preview=Environment.GetEnvironmentVariable("QUIC_UI_PREVIEW");
            if(!string.IsNullOrEmpty(preview)) {window.UpdateLayout();var bitmap=new System.Windows.Media.Imaging.RenderTargetBitmap((int)window.ActualWidth,(int)window.ActualHeight,96,96,System.Windows.Media.PixelFormats.Pbgra32);bitmap.Render(window);var encoder=new System.Windows.Media.Imaging.PngBitmapEncoder();encoder.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bitmap));using var image=System.IO.File.Create(preview);encoder.Save(image);}
            window.Close(); window=null;
            Check((await client.SendAsync("status")).State=="running","UI close stopped VPN");
            window=new MainWindow(client);window.Show();await Ready(window);
            await Click(window,"ToggleButton");Check(Text(window,"StateText")=="Отключено","real Stop failed");
            var toggle=(Button)window.FindName("ToggleButton");toggle.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(30);
            Check(toggle.IsEnabled && toggle.Content.ToString()=="Отменить","live startup not cancellable");toggle.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
            var until=DateTime.UtcNow.AddSeconds(25);while(DateTime.UtcNow<until&&!Text(window,"MessageText").Contains("подключение отменено")) await Task.Delay(50);
            Check(Text(window,"MessageText").Contains("подключение отменено")&&(await client.SendAsync("status")).State=="stopped","live cancellation not confirmed");
            Console.WriteLine($"LIVE PASS: cancellation, owned route, HTTPS expected exit, UDP/TCP DNS, TX={running.Runtime!.Traffic!.TxBytes}, RX={running.Runtime.Traffic.RxBytes}, UI close/reopen/Stop");
        }
        finally
        {
            window?.Close();var stopped=await client.SendAsync("stop");Check(stopped.Ok&&stopped.State=="stopped","cleanup Stop failed");
        }
    }
}
