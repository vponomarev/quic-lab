using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

static class ServerExitAcceptance {
 static void Check(bool ok,string message){if(!ok)throw new Exception(message);}
 public static async Task Run(){
  string token=new('a',64);
  string raw=JsonSerializer.Serialize(new{version=2,kind="vpn",name="test",hostname="vpn.example",transports=new[]{"quic","https","awg","vless"},quic="vpn.example:443",https="vpn.example:8443",certificate="cert",key="key",awg_config="awg",vless_uri="vless",device_id="device",config_url="https://control.example/api/v1/devices/device/config",update_token=token});
  var profiles=UrlProfileLoader.Parse(raw).Profiles;
  Check(profiles.All(p=>p.ServerControl?.Token==token),"control credentials lost for transport");
  profiles[0].ServerControl!.Token="changed";Check(profiles[1].ServerControl!.Token==token,"control metadata aliased");
  try{UrlProfileLoader.Parse(raw.Replace("devices/device/config","devices/other/config"));throw new Exception("wrong device URL accepted");}catch(System.IO.InvalidDataException){}
  var client=new Fake();var w=new SettingsWindow(client);w.Show();await Task.Delay(250);
  var refresh=(Button)w.FindName("RefreshServerExitsButton");var apply=(Button)w.FindName("ApplyServerExitButton");
  Check(refresh.IsEnabled&&!w.HasChanges,"control load made draft dirty");
  refresh.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  var box=(ComboBox)w.FindName("ServerExitBox");Check(box.Items.Count==2&&box.SelectedIndex==1,"allowed choices/default");
  box.SelectedIndex=0;apply.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  Check(client.Last?.Change?.ID==""&&client.Last.Change.Revision==new string('b',64)&&client.Last.ExpectedRevision==7,"default selection lost revisions");
  client.Fail=true;apply.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(100);
  Check(!apply.IsEnabled&&((TextBlock)w.FindName("ServerExitState")).Text.Contains("Обновите"),"conflict left stale apply enabled");
  var preview=Environment.GetEnvironmentVariable("QUIC_EXIT_UI_PREVIEW");
  if(!string.IsNullOrEmpty(preview)){((TabItem)((TabControl)w.FindName("Tabs")).Items[0]).IsSelected=true;w.UpdateLayout();var bmp=new System.Windows.Media.Imaging.RenderTargetBitmap((int)w.ActualWidth,(int)w.ActualHeight,96,96,System.Windows.Media.PixelFormats.Pbgra32);bmp.Render(w);var enc=new System.Windows.Media.Imaging.PngBitmapEncoder();enc.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bmp));using var file=System.IO.File.Create(preview);enc.Save(file);}
  w.Close();Console.WriteLine("Server exits WPF: import all transports, choices/default, revision conflict PASS");
 }
 sealed class Fake:ISettingsClient,IServerExitClient {
  public ServerExitRequest? Last;public bool Fail;
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default)=>Task.FromResult(new SettingsReply(new(true,"stopped","",0,null,["settings-v1","server-exits-v1"],7),new(){Revision=7,HasKey=true,Settings=new(){Name="Сервер",Endpoint="192.0.2.1:443",Hostname="test",Certificate="cert",InterfaceIndex=5,Targets=["198.18.0.1/32"],RoutingMode="subnets",UplinkMode="manual",Dns="1.1.1.1",ServerControl=new(){DeviceID="device",ConfigURL="https://control.example/api/v1/devices/device/config"}}},""));
  public Task<ServerExitReply> ServerExits(ServerExitRequest request,CancellationToken ct=default){Last=request;return Task.FromResult(new ServerExitReply(new(!Fail,"stopped",Fail?"exit_revision_conflict":"",0),Fail?null:new(){Selected=request.Change?.ID??"west",Effective="west",Revision=new('b',64),Choices=[new(){ID="west",Name="Запад",Protocol="awg"},new(){ID="east",Name="Восток",Protocol="awg",Disabled=true}]}));}
 }
}
