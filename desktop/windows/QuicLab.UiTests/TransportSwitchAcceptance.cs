using QuicLab.Windows;
using System.Windows;
using System.Windows.Controls;

static class TransportSwitchAcceptance {
 public static async Task Run() {
  var client=new Client();
  var profiles=new List<EngineSettings>{
   new(){Transport="quic",Endpoint="quic.example:443",Hostname="quic.example",Certificate="cert",Key="quic-key"},
   new(){Transport="https",Endpoint="https.example:8443",Hostname="https.example",Certificate="cert",Key="https-key",CA="https-ca"},
   new(){Transport="awg",Endpoint="awg.example:51820",Key="awg-profile"}
  };
  var w=new SettingsWindow(client,(_,_)=>Task.FromResult(new UrlProfileBundle(profiles)));w.Show();await Task.Delay(200);
  ((TextBox)w.FindName("UrlBox")).Text="https://example.test/profile";
  ((Button)w.FindName("LoadUrlButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  var transport=(ComboBox)w.FindName("TransportBox");var downloaded=(ComboBox)w.FindName("DownloadedTransportBox");
  transport.SelectedIndex=2;
  Check(((PasswordBox)w.FindName("KeyBox")).Password=="awg-profile"&&((TextBox)w.FindName("HostBox")).Text=="awg.example","lower AWG selector retained QUIC credentials/address");
  Check(downloaded.SelectedItem==profiles[2],"selectors not synchronized");
  Check(((TextBox)w.FindName("CABox")).Text=="","AWG retained trust");
  ((PasswordBox)w.FindName("KeyBox")).Password="edited-awg-profile";
  client.FailNextStart=true;
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  Check(client.Saved?.Settings.Key=="edited-awg-profile"&&client.Saved.Settings.Transport=="awg","wrong AWG save");
  Check(client.Saved!.Settings.Targets.SequenceEqual(new[]{"1.1.1.0/30"})&&client.Saved.Settings.InterfaceIndex==5,"switch changed local routing");
  transport.SelectedIndex=1;
  Check(((PasswordBox)w.FindName("KeyBox")).Password=="https-key"&&((TextBox)w.FindName("PortBox")).Text=="8443","saved profile lost alternatives");
  Check(((TextBox)w.FindName("CABox")).Text=="https-ca","selection cleared newly loaded trust");
  downloaded.SelectedItem=profiles[0];
  Check(transport.SelectedIndex==0&&((PasswordBox)w.FindName("KeyBox")).Password=="quic-key","upper selector regressed");
  transport.SelectedIndex=3;
  ((Button)w.FindName("CheckButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  Check(((TextBlock)w.FindName("Feedback")).Text.Contains("профиль",StringComparison.OrdinalIgnoreCase),"missing transport profile has no guidance");
  // Return to a valid supplied profile and save so closing does not prompt.
  transport.SelectedIndex=2;
  Check(((PasswordBox)w.FindName("KeyBox")).Password=="edited-awg-profile","switch reverted saved credential edit");
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  client.RejectProfile=true;
  ((Button)w.FindName("CheckButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  var feedback=((TextBlock)w.FindName("Feedback")).Text;
  Check(feedback.Contains("AWG/VLESS")&&!feedback.Contains("сертификат"),"profile failure still blames certificate");
  w.Close();
  Console.WriteLine("Transport switching: credentials, endpoint, trust, save/reselect and local routing PASS");
 }
 static void Check(bool ok,string reason){if(!ok)throw new Exception(reason);}
 sealed class Client:ISettingsClient,ITransportImportClient {
  public SettingsChange? Saved;
  public bool RejectProfile;
  public bool FailNextStart;
  SettingsView view=new(){Revision=1,HasKey=true,Settings=new(){Transport="quic",Endpoint="old.example:443",Hostname="old.example",Certificate="old-cert",InterfaceIndex=5,Targets=["1.1.1.0/30"]}};
  public Task<TransportImport> PreviewImport(string kind,string raw,CancellationToken ct=default)=>Task.FromResult(new TransportImport{Endpoint="awg.example:51820"});
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){
   if(command=="validate_settings"&&RejectProfile)return Task.FromResult(new SettingsReply(new Reply(false,"stopped","invalid_config",0),null,"credentials: unsupported_or_invalid_profile"));
   if(command=="apply_settings"&&change!=null){Saved=change;view=new(){Revision=view.Revision+1,HasKey=true,Settings=System.Text.Json.JsonSerializer.Deserialize<EngineSettings>(System.Text.Json.JsonSerializer.Serialize(change.Settings,SettingsDraft.Json),SettingsDraft.Json)!};view.Settings.Key="";}
   if(command=="apply_settings"&&FailNextStart){FailNextStart=false;return Task.FromResult(new SettingsReply(new Reply(false,"stopped","backend_start_failed",0),null,""));}
   return Task.FromResult(new SettingsReply(new Reply(true,"stopped","",0),view,""));
  }
 }
}
