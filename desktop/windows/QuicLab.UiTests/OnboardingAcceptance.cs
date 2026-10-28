using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

partial class UiTests
{
    static async Task OnboardingAcceptance()
    {
        var client = new FreshClient();
        var window = new MainWindow(client); window.Show();
        try {
            await Ready(window);
            Check(Text(window,"StateText").Contains("настро"), "fresh service must explain setup requirement");
            var button=(Button)window.FindName("ToggleButton");
            Check(button.Content.ToString()=="Настроить подключение", "fresh service offers configuration instead of Start");
            Check(((Button)window.FindName("SettingsButton")).IsEnabled, "first-run editor available");
            var editor=new SettingsWindow(client);editor.Show();await Task.Delay(200);
            try {
                Check(((ComboBox)editor.FindName("RoutingModeBox")).SelectedIndex==0, "new profile defaults to all IPv4");
                Check(((CheckBox)editor.FindName("AutoUplinkBox")).IsChecked==true, "new profile defaults to automatic uplink");
                Check(((Button)editor.FindName("LoadUrlButton")).IsEnabled, "first-run import enabled");
                Check(((CheckBox)editor.FindName("ReplaceKeyBox")).IsChecked==true, "first key entry enabled automatically");
            } finally {editor.Close();}
            Check(client.Starts==0,"onboarding must not start VPN");
        } finally {window.Close();}
        Console.WriteLine("First-run dashboard and import editor PASS");
    }
    sealed class FreshClient:IStatusClient,ISettingsClient
    {
        public int Starts;
        Reply Status=>new(true,"stopped","",0,new RuntimeInfo("unconfigured","","",0,null),["settings-v1"]);
        public Task<Reply> SendAsync(string command,JsonElement? config=null,CancellationToken cancellationToken=default){if(command=="start")Starts++;return Task.FromResult(Status);}
        public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default)=>Task.FromResult(new SettingsReply(Status,new SettingsView{Revision=0,HasKey=false,Settings=new EngineSettings{Transport="quic",RoutingMode="all_ipv4",UplinkMode="auto",Targets=[]}},""));
    }
}
