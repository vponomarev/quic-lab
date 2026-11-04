using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using QuicLab.Windows;

static class ProfileLibraryAcceptance {
 static void Check(bool x,string why){if(!x)throw new Exception(why);}
 public static async Task Run(){
  var old=new Fake(false);var legacy=new SettingsWindow(old);legacy.Show();await Task.Delay(200);
  Check(!((Button)legacy.FindName("SaveNewProfileButton")).IsEnabled&&old.LibraryCalls==0,"old service capability gate");legacy.Close();
  var client=new Fake(true);var w=new SettingsWindow(client);w.Show();await Task.Delay(200);
  ((TabItem)w.FindName("ProfilesTab")).IsSelected=true;
  ((Button)w.FindName("SaveNewProfileButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  Check(client.Profiles.Count==1&&client.Active.Endpoint=="192.0.2.1:443"&&!w.HasChanges,"save mutated active/draft or revision");w.Close();
  client.Active.Endpoint="192.0.2.2:443";client.Revision++;
  w=new SettingsWindow(client);w.Show();await Task.Delay(200);((TabItem)w.FindName("ProfilesTab")).IsSelected=true;
  var list=(ListBox)w.FindName("SavedProfilesList");Check(list.Items.Count==1,"reopen lost library");list.SelectedIndex=0;
  var preview=Environment.GetEnvironmentVariable("QUIC_PROFILE_UI_PREVIEW");
  if(!string.IsNullOrEmpty(preview)){w.UpdateLayout();var bmp=new System.Windows.Media.Imaging.RenderTargetBitmap((int)w.ActualWidth,(int)w.ActualHeight,96,96,System.Windows.Media.PixelFormats.Pbgra32);bmp.Render(w);var enc=new System.Windows.Media.Imaging.PngBitmapEncoder();enc.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bmp));using var image=System.IO.File.Create(preview);enc.Save(image);}
  ((Button)w.FindName("ActivateProfileButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  Check(client.Active.Endpoint=="192.0.2.1:443"&&!client.ActivatedWithReconnect&&((TextBox)w.FindName("HostBox")).Text=="192.0.2.1","stopped activation/reload");w.Close();
  var concurrent=new Fake(true){InterleaveSave=true};w=new SettingsWindow(concurrent);w.Show();await Task.Delay(200);
  ((Button)w.FindName("SaveNewProfileButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
  Check(concurrent.LastExpected==2&&concurrent.Active.Name=="Другой редактор"&&((TextBlock)w.FindName("Feedback")).Text.Contains("уже изменены"),"library response adopted another editor revision and overwrote its settings");w.Close();
  foreach(bool activate in new[]{false,true}){
   var timeout=new Fake(true){TimeoutMutation=true};if(activate){var saved=new EngineSettings{Name="Резервный",Endpoint="192.0.2.2:443"};timeout.Profiles.Add(saved);}
   w=new SettingsWindow(timeout);w.Show();await Task.Delay(200);
   if(activate)((ListBox)w.FindName("SavedProfilesList")).SelectedIndex=0;
   ((Button)w.FindName(activate?"ActivateProfileButton":"SaveNewProfileButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));await Task.Delay(200);
   Check(((TextBlock)w.FindName("Feedback")).Text.Contains("неизвестен")&&((Button)w.FindName("SaveButton")).IsEnabled,"timeout after commit was hidden");w.Close();
  }
  Console.WriteLine("Profile library WPF: capability gate, save/reopen, revision and stopped activation PASS");
 }
 sealed class Fake(bool supported):ISettingsClient,IProfileLibraryClient {
  public ulong Revision=1,LastExpected;public int LibraryCalls;public bool ActivatedWithReconnect,InterleaveSave,TimeoutMutation;
  public EngineSettings Active=new(){Name="Основной сервер",Endpoint="192.0.2.1:443",Hostname="test",Certificate="cert",InterfaceIndex=5,Targets=["198.18.0.1/32"],RoutingMode="subnets",UplinkMode="manual",Dns="1.1.1.1"};
  public List<EngineSettings> Profiles=[];
  Reply Status()=>new(true,"stopped","",0,null,supported?["settings-v1","profile-library-v1"]:["settings-v1"],Revision);
  public Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){
   if(change!=null){LastExpected=change.ExpectedRevision;if(change.ExpectedRevision!=Revision)return Task.FromResult(new SettingsReply(Status() with {Ok=false,Error="revision_conflict"},null,""));}
   if(command=="apply_settings"){Active=change!.Settings;Revision++;}
   return Task.FromResult(new SettingsReply(Status(),new(){Revision=Revision,Settings=Active,HasKey=true},""));
  }
  public Task<ProfileLibraryReply> SendProfileAsync(string command,ProfileChange? change=null,CancellationToken cancellationToken=default){
   LibraryCalls++;if(command=="save_profile"){Profiles.Add(JsonSerializer.Deserialize<EngineSettings>(JsonSerializer.Serialize(change!.Settings,SettingsDraft.Json),SettingsDraft.Json)!);Revision++;}
   if(command=="activate_profile"){Active=Profiles[0];ActivatedWithReconnect=change!.Reconnect;Revision++;}
   if(command=="save_profile"&&InterleaveSave){Active.Name="Другой редактор";Revision++;InterleaveSave=false;}
   if(command!="list_profiles"&&TimeoutMutation)throw new OperationCanceledException("reply deadline after commit");
   return Task.FromResult(new ProfileLibraryReply(Status(),new(){Revision=Revision,Profiles=Profiles.Select(p=>new ProfileSummary{ID="0123456789abcdef0123456789abcdef",Name=p.Name,Transport=p.Transport,Endpoint=p.Endpoint}).ToArray()}));
  }
 }
}
