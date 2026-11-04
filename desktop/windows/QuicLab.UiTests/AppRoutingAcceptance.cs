using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

static class AppRoutingAcceptance {
 static void Check(bool value,string why){if(!value)throw new Exception(why);}
 public static async Task Run(){
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
  Console.WriteLine("Per-app WPF: independent lists, capability gate, save/reload, scope preview PASS");
 }
 sealed class Fake(bool supported,AppRoutingPolicy policy):ISettingsClient{
  public EngineSettings? Saved;ulong revision=1;
  EngineSettings settings=new(){Endpoint="192.0.2.1:443",Hostname="test",Certificate="cert",InterfaceIndex=5,Targets=["1.1.1.1/32"],AppRouting=supported?policy:null};
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){
   if(command=="apply_settings"){Saved=settings=change!.Settings;revision++;}
   return Task.FromResult(new SettingsReply(new Reply(true,"stopped","",0,null,supported?["settings-v1","app-routing-tun-v2"]:["settings-v1"],revision),new(){Revision=revision,HasKey=true,Settings=settings},""));
  }
 }
}
