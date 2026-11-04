using System.IO;
using System.Windows;
namespace QuicLab.Windows;

public partial class SettingsWindow {
 ApplicationSelection applications=new();
 AppRoutingPolicy? originalAppPolicy;
 bool appRoutingSupported,appsTouched;
 public void SelectApplicationsTab()=>ApplicationsTab.IsSelected=true;
 void LoadAppRouting(AppRoutingPolicy? policy,string[]? capabilities){
  originalAppPolicy=policy;applications=ApplicationSelection.FromPolicy(policy);appsTouched=false;
  appRoutingSupported=capabilities?.Contains("app-routing-tun-v2")==true;
  AppRoutingEnabled.IsEnabled=EditApplicationsButton.IsEnabled=UnknownAppChoice.IsEnabled=appRoutingSupported;
  AppRoutingEnabled.IsChecked=policy?.Enabled==true;
  UnknownAppChoice.SelectedIndex=policy?.Unknown switch {"direct"=>1,"block"=>2,_=>0};
  appsTouched=false;UpdateAppPreview();
 }
 AppRoutingPolicy? CaptureAppRouting(){
  if(!appRoutingSupported)return originalAppPolicy;
  if(originalAppPolicy==null&&!appsTouched&&AppRoutingEnabled.IsChecked!=true)return null;
  return applications.ToPolicy(AppRoutingEnabled.IsChecked==true,UnknownAppChoice.SelectedIndex switch {1=>"direct",2=>"block",_=>"tunnel"});
 }
 void AppRoutingChanged(object sender,RoutedEventArgs e){if(!loaded)return;appsTouched=true;UpdateAppPreview();}
 void UpdateAppPreview(){
  if(AppRoutingPreview==null)return;
  if(!appRoutingSupported){AppRoutingPreview.Text="Для маршрутизации по приложениям обновите службу. Сохранение обычных настроек доступно.";return;}
  var p=applications.ToPolicy(true,"tunnel");
  string mode=p.Mode==ApplicationSelection.OnlyMode?$"VPN только для выбранных ({p.Only.Length}); остальные — напрямую.":$"VPN для всех, кроме выбранных ({p.Except.Length}); исключённые — напрямую.";
  string capture=RoutingModeBox.SelectedIndex==0?"Область захвата: весь IPv4, кроме локальных сетей, специальных маршрутов и адреса VPN-сервера.":"Область захвата: только подсети из вкладки «Маршрутизация». Остальные адреса не зависят от списков приложений.";
  AppRoutingPreview.Text=(AppRoutingEnabled.IsChecked==true?mode:"Маршрутизация по приложениям выключена; списки сохраняются.")+"\n"+capture+"\nDNS в захваченной области — через VPN для всех приложений. IPv6 не изменяется. Direct проходит через локальный движок. При переподключении службы прерываются и VPN-, и direct-потоки; при остановке обычная маршрутизация восстанавливается.";
 }
 void EditApplicationsClick(object sender,RoutedEventArgs e){
  var editor=new ApplicationSelectionWindow(applications){Owner=this};editor.ShowDialog();
  if(editor.SelectionResult is {} result){applications=result;appsTouched=true;UpdateAppPreview();}
 }
 async void ImportApplicationDraftClick(object sender,RoutedEventArgs e){
  if(!appRoutingSupported||busy)return;Busy(true);
  try{var file=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"QUIC Lab","application-selection-draft.json");if(!File.Exists(file))throw new FileNotFoundException();applications=await ApplicationSelection.LoadAsync(file);appsTouched=true;UpdateAppPreview();Feedback.Text="Черновик списков загружен. Проверьте режим, включите маршрутизацию и сохраните настройки.";}
  catch(Exception){Feedback.Text="Не удалось прочитать сохранённый черновик приложений.";}finally{Busy(false);}
 }
}
