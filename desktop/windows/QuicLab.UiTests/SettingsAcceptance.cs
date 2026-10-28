using QuicLab.Windows;
using System.Windows;
using System.Windows.Controls;

static class SettingsAcceptance {
 public static void RunModel(){
  var rows=Enumerable.Range(0,256).Select(i=>$"198.18.0.{i}/32").ToArray();
  if(SettingsDraft.ValidateRoutes(rows,"192.0.2.1")!=null)throw new Exception("256 routes rejected");
  if(SettingsDraft.ValidateRoutes(rows.Append("198.18.1.0/32").ToArray(),"192.0.2.1")==null)throw new Exception("257 accepted");
  foreach(var bad in new[]{new[]{"198.18.0.1/24"},new[]{"198.18.0.0/24","198.18.0.1/32"},new[]{"192.0.2.1/32"},new[]{"0.0.0.0/0"},new[]{"::/0"}})
   if(SettingsDraft.ValidateRoutes(bad,"192.0.2.1")==null)throw new Exception("bad route accepted");
  Console.WriteLine("Settings draft: 256 limit, CIDR, overlap, endpoint PASS");
 }


 public static async Task RunLive(bool many=false){
  var client=new PipeClient();var initial=await client.SendSettingsAsync("read_settings");if(!initial.Status.Ok||initial.View==null)throw new Exception("settings capability absent");
  if(initial.Status.State!="stopped")throw new Exception("stop VPN before acceptance");
  var original=initial.View.Settings;var cloned=System.Text.Json.JsonSerializer.Deserialize<EngineSettings>(System.Text.Json.JsonSerializer.Serialize(original,SettingsDraft.Json),SettingsDraft.Json)!;
  if(!original.Targets.SequenceEqual(new[]{"1.1.1.0/30"}))throw new Exception("unexpected test routes");
  try{
   cloned.Targets=many?new[]{"1.1.1.0/30"}.Concat(Enumerable.Range(0,255).Select(i=>$"198.18.0.{i}/32")).ToArray():["1.1.1.0/31","1.1.1.2/31"];cloned.Name="Проверка редактора";
   var c=new SettingsChange{ExpectedRevision=initial.View.Revision,Settings=cloned};
   var r=await client.SendSettingsAsync("apply_settings",c);if(!r.Status.Ok)throw new Exception("save: "+r.Status.Error);
   var stale=await client.SendSettingsAsync("apply_settings",c);if(stale.Status.Error!="revision_conflict")throw new Exception("stale editor accepted");
   var restarted=new PipeClient();var view=await restarted.SendSettingsAsync("read_settings");if(view.View?.Settings.Targets.Length!=(many?256:2)||!view.View.HasKey||view.View.Settings.Key!="")throw new Exception("read/preserve/redaction");
   var started=await restarted.SendAsync("start");if(!started.Ok||started.Runtime?.RouteCount!=(many?256:2))throw new Exception("edited profile start: "+started.Error);
   using var ping=new System.Net.NetworkInformation.Ping();var echo=await ping.SendPingAsync("1.1.1.0",4000);if(echo.Status!=System.Net.NetworkInformation.IPStatus.Success)throw new Exception("edited routes ICMP");
   Console.WriteLine($"Live settings: save/read, conflict, unchanged key, {(many?256:2)} routes, connect/ping PASS");
  }finally{await client.SendAsync("stop");var view=await client.SendSettingsAsync("read_settings");if(view.View==null)throw new Exception("cannot restore profile");var restored=await client.SendSettingsAsync("apply_settings",new(){ExpectedRevision=view.View.Revision,Settings=original});if(!restored.Status.Ok)throw new Exception("restore: "+restored.Status.Error);}
 }
 public static async Task RunUI(){
  var client=new FakeSettings();var w=new SettingsWindow(client,(_,_)=>Task.FromResult(UrlProfileLoader.Parse("""{"version":1,"kind":"vpn","hostname":"loaded.example","name":"Loaded","quic":"loaded.example:443","https":"loaded.example:8443","key":"new-key","certificate":"new-cert"}""")));w.Show();await Task.Delay(200);
  if(w.HasChanges)throw new Exception("unchanged editor dirty");
  if(((ComboBox)w.FindName("RoutingModeBox")).SelectedIndex!=1)throw new Exception("legacy routing upgraded silently");
  ((ComboBox)w.FindName("RoutingModeBox")).SelectedIndex=0;((CheckBox)w.FindName("AutoUplinkBox")).IsChecked=true;
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  if(client.View.Settings.RoutingMode!="all_ipv4"||client.View.Settings.UplinkMode!="auto")throw new Exception("full routing save failed");
  ((ComboBox)w.FindName("RoutingModeBox")).SelectedIndex=1;((CheckBox)w.FindName("AutoUplinkBox")).IsChecked=false;
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  client.View.Revision=1; // Remaining tests use their original revision baseline.
  w.Close();w=new SettingsWindow(client,(_,_)=>Task.FromResult(UrlProfileLoader.Parse("""{"version":1,"kind":"vpn","hostname":"loaded.example","name":"Loaded","quic":"loaded.example:443","https":"loaded.example:8443","key":"new-key","certificate":"new-cert"}""")));w.Show();await Task.Delay(200);

  var name=(TextBox)w.FindName("NameBox");name.Text="edited";if(!w.HasChanges)throw new Exception("edit not dirty");
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  if(client.View.Settings.Name!="edited"||client.View.Revision!=2||w.HasChanges)throw new Exception("save/reload failed");
  if(Environment.GetEnvironmentVariable("QUIC_LAB_SETTINGS_PREVIEW") is string file){w.UpdateLayout();var bitmap=new System.Windows.Media.Imaging.RenderTargetBitmap((int)w.ActualWidth,(int)w.ActualHeight,96,96,System.Windows.Media.PixelFormats.Pbgra32);bitmap.Render(w);var enc=new System.Windows.Media.Imaging.PngBitmapEncoder();enc.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bitmap));using var stream=System.IO.File.Create(file);enc.Save(stream);}
  ((TextBox)w.FindName("UrlBox")).Text="https://example.test/profile";
  ((Button)w.FindName("LoadUrlButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  if(client.View.Revision!=2||!w.HasChanges||((TextBox)w.FindName("HostBox")).Text!="loaded.example")throw new Exception("URL changed service or failed to fill draft");
  ((ComboBox)w.FindName("DownloadedTransportBox")).SelectedIndex=1;
  if(((TextBox)w.FindName("PortBox")).Text!="8443")throw new Exception("downloaded transport selection");
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  if(client.View.Revision!=3||client.View.Settings.Transport!="https"||!client.View.Settings.Targets.SequenceEqual(new[]{"1.1.1.0/30"})||client.View.Settings.InterfaceIndex!=5)throw new Exception("URL import altered local routes/interface");
  var ca=(TextBox)w.FindName("CABox");ca.Text="old QUIC trust";((ComboBox)w.FindName("TransportBox")).SelectedIndex=3;
  if(ca.Text!=""||((FrameworkElement)w.FindName("CaFields")).Visibility!=Visibility.Visible)throw new Exception("transport change retained hidden trust or hid VLESS CA");
  ((ComboBox)w.FindName("TransportBox")).SelectedIndex=1;ca.Text="";
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  w.Close();
  var conflicting=new FakeSettings{Conflict=true};var stale=new SettingsWindow(conflicting);stale.Show();await Task.Delay(200);
  var staleName=(TextBox)stale.FindName("NameBox");string originalName=staleName.Text;staleName.Text="stale edit";
  for(int i=0;i<2;i++){((Button)stale.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);}
  bool protectedDraft=stale.HasChanges&&conflicting.Expected.SequenceEqual(new ulong[]{1,1});
  staleName.Text=originalName;
  if(!stale.HasChanges)stale.Close();else stale.Hide();
  if(!protectedDraft)throw new Exception("apply conflict adopted a newer revision or marked stale draft clean");
  Console.WriteLine("Settings conflict: stale draft cannot overwrite newer revision PASS");
  Console.WriteLine("Settings UI: load, dirty tracking, save/reload PASS");
 }
 sealed class FakeSettings:ISettingsClient {
  public bool Conflict;public List<ulong> Expected=new();
  public SettingsView View=new(){Revision=1,HasKey=true,Settings=new(){Name="Проверка QUIC",Endpoint="192.0.2.1:443",Hostname="test.example",Certificate="-----BEGIN CERTIFICATE-----",InterfaceIndex=5,Targets=["1.1.1.0/30"]}};
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){if(command=="apply_settings"&&change!=null){Expected.Add(change.ExpectedRevision);if(Conflict)return Task.FromResult(new SettingsReply(new Reply(false,"stopped","revision_conflict",0,null,["settings-v1"],2),null,""));View.Settings=change.Settings;View.Revision++;}return Task.FromResult(new SettingsReply(new Reply(true,"stopped","",0,null,["settings-v1"],View.Revision),View,""));}
 }
}
