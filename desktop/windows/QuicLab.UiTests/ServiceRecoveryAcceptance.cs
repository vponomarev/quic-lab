using QuicLab.Windows;
partial class UiTests
{
    static async Task ServiceRecoveryAcceptance()
    {
        var client=new FakeClient { Fail=true };var window=new MainWindow(client);window.Show();
        try {
            await Task.Delay(300);Check(Text(window,"StateText")=="Нет связи со службой","initial unavailable state");
            client.Fail=false;
            await Task.Delay(1800);
            Check(Text(window,"StateText")=="Отключено","automatic service reconnect");
            Check(!Text(window,"MessageText").Contains("недоступна"),"stale unavailable error after recovery");
            Check(client.State=="stopped","reconnecting UI must not start VPN");
            Console.WriteLine("Service recovery without stale errors or VPN autostart PASS");
        } finally { window.Close(); }
    }
}
