using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

static class AppRoutingAcceptance {
 static void Check(bool value,string why){if(!value)throw new Exception(why);}
 public static async Task Run(){
  var draft=new SettingsChange{Settings=new(){Endpoint="192.0.2.1:443",AppRouting=new(){Enabled=true}}};
  string connection=SettingsDraft.ConnectionFingerprint(draft);
  draft.Settings.AppRouting.Enabled=false;
  Check(SettingsDraft.ConnectionFingerprint(draft)==connection,"app-only change requires reconnect");
  draft.Settings.Endpoint="192.0.2.2:443";
  Check(SettingsDraft.ConnectionFingerprint(draft)!=connection,"endpoint change treated as hot");
  var selection=new ApplicationSelection();selection.Add(@"C:\app\selected.exe");selection.Mode=ApplicationSelection.ExceptMode;selection.Add(@"C:\app\excluded.exe");
  var policy=selection.ToPolicy(true,"block");var round=ApplicationSelection.FromPolicy(policy).ToPolicy(true,"block");
  Check(round.Only.SequenceEqual(policy.Only)&&round.Except.SequenceEqual(policy.Except)&&round.Mode==policy.Mode,"two remembered lists");
  policy.Only[0]=@"C:\changed.exe";Check(round.Only[0]==@"C:\app\selected.exe","UI policy alias");
  Check(!JsonSerializer.Serialize(new EngineSettings(),SettingsDraft.Json).Contains("app_routing"),"legacy service receives unknown field");
  foreach(var supported in new[]{false,true}){
   var client=new Fake(supported,round);var w=new SettingsWindow(client);w.Show();await Task.Delay(200);
   var enabled=(CheckBox)w.FindName("AppRoutingEnabled");Check(enabled.IsEnabled==supported,"capability gate");
   if(supported){
    Check(enabled.IsChecked==true,"stored enabled policy");
    ((ComboBox)w.FindName("UnknownAppChoice")).SelectedIndex=1;
    ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
    Check(client.Saved?.AppRouting?.Unknown=="direct"&&client.Saved.AppRouting.Except[0]==@"C:\app\excluded.exe","saved app policy");
    Check(((TextBlock)w.FindName("AppRoutingPreview")).Text.Contains("IPv6"),"capture preview");
   }
   w.Close();
  }
  var live=new Fake(true,round,true);var window=new SettingsWindow(live);window.Show();await Task.Delay(200);
  ((ComboBox)window.FindName("UnknownAppChoice")).SelectedIndex=1;
  ((Button)window.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  Check(live.Saved!=null&&!live.Reconnect,"app-only save restarted running VPN");window.Close();
  Console.WriteLine("Per-app WPF: lists, capability gate, save/reload, hot running apply and preview PASS");
 }
 sealed class Fake(bool supported,AppRoutingPolicy policy,bool live=false):ISettingsClient{
  public EngineSettings? Saved;public bool Reconnect;ulong revision=1;
  EngineSettings settings=new(){Endpoint="192.0.2.1:443",Hostname="test",Certificate="cert",InterfaceIndex=5,Targets=["1.1.1.1/32"],AppRouting=supported?policy:null,RoutingMode="subnets",UplinkMode="manual",Dns="1.1.1.1"};
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){
   if(command=="apply_settings"){Saved=settings=change!.Settings;Reconnect=change.Reconnect;revision++;}
   return Task.FromResult(new SettingsReply(new Reply(true,live?"running":"stopped","",0,null,live?["settings-v1","app-routing-tun-v2","app-routing-live-v1"]:supported?["settings-v1","app-routing-tun-v2"]:["settings-v1"],revision),new(){Revision=revision,HasKey=true,Settings=settings},""));
  }
 }
}
