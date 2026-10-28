using System.IO;
using System.Windows;
using System.Windows.Controls;
namespace QuicLab.Windows;
public partial class SettingsWindow {
 readonly Func<string,CancellationToken,Task<UrlProfileBundle>> download;
 CancellationTokenSource? downloadCancellation;
 bool selectingDownloadedTransport;
 static async Task<UrlProfileBundle> DownloadProfile(string url,CancellationToken ct){using var http=UrlProfileLoader.CreateHttpClient();return await new UrlProfileLoader(http).Load(url,ct);}
 async void LoadUrlClick(object sender,RoutedEventArgs e){
  if(busy||!loaded)return;
  using var cancel=CancellationTokenSource.CreateLinkedTokenSource(lifetime.Token);downloadCancellation=cancel;
  Busy(true);CancelDownloadButton.Visibility=Visibility.Visible;Feedback.Text="Загружаю и проверяю параметры…";
  try{
   var bundle=await download(UrlBox.Text,cancel.Token);
   foreach(var s in bundle.Profiles){if(s.Transport is "awg" or "vless"){
    if(client is not ITransportImportClient importer)throw new InvalidDataException("Обновите службу для импорта AWG/VLESS.");
    var meta=await importer.PreviewImport(s.Transport,s.Key,cancel.Token);s.Endpoint=meta.Endpoint;if(s.Name.Length==0)s.Name=meta.Name;
   }}
   cancel.Token.ThrowIfCancellationRequested();
   string selected=(TransportBox.SelectedItem as ComboBoxItem)?.Tag.ToString()??"quic";
   DownloadedTransportBox.ItemsSource=bundle.Profiles;DownloadedTransportPanel.Visibility=bundle.Profiles.Count>1?Visibility.Visible:Visibility.Collapsed;
   DownloadedTransportBox.SelectedItem=bundle.Profiles.FirstOrDefault(s=>s.Transport==selected)??bundle.Profiles[0];
   UrlBox.Clear();Feedback.Text="Параметры загружены в черновик. Проверьте транспорт и адрес, затем нажмите «Сохранить».";
  }catch(OperationCanceledException){Feedback.Text=cancel.IsCancellationRequested?"Загрузка отменена. Прежние настройки сохранены.":"Сервер не ответил вовремя. Можно повторить загрузку.";}
  catch(InvalidDataException ex){Feedback.Text=ex.Message;}
  catch(Exception){Feedback.Text="Не удалось загрузить профиль. Проверьте ссылку, доступность сервера и его HTTPS-сертификат.";}
  finally{downloadCancellation=null;CancelDownloadButton.Visibility=Visibility.Collapsed;Busy(false);}
 }
 void CancelDownloadClick(object sender,RoutedEventArgs e)=>downloadCancellation?.Cancel();
 void DownloadedTransportChanged(object sender,SelectionChangedEventArgs e){
  if(!loaded||selectingDownloadedTransport||DownloadedTransportBox.SelectedItem is not EngineSettings s)return;
  selectingDownloadedTransport=true;
  try {
  TransportBox.SelectedIndex=s.Transport switch{"https"=>1,"awg"=>2,"vless"=>3,_=>0};
  int colon=s.Endpoint.LastIndexOf(':');HostBox.Text=colon<0?s.Endpoint:s.Endpoint[..colon];PortBox.Text=colon<0?"443":s.Endpoint[(colon+1)..];
  NameBox.Text=s.Name;HostnameBox.Text=s.Hostname;CertificateBox.Text=s.Certificate;CABox.Text=s.CA;ServerNameBox.Text=s.ServerName;VerifyNameBox.Text=s.VerifyName;
  KeyBox.Password=s.Key;ReplaceKeyBox.IsChecked=true;ImportHint.Text="Загружено по URL. Подсети и физический адаптер оставлены прежними.";
  } finally {selectingDownloadedTransport=false;}
 }
 void SelectTransportProfile(){
  if(!loaded||selectingDownloadedTransport)return;
  var kind=(TransportBox.SelectedItem as ComboBoxItem)?.Tag.ToString();
  var profile=DownloadedTransportBox.Items.OfType<EngineSettings>().FirstOrDefault(s=>s.Transport==kind);
  if(profile!=null){
   DownloadedTransportBox.SelectedItem=profile;
  } else if(DownloadedTransportBox.Items.Count>0){
   DownloadedTransportBox.SelectedItem=null;
   KeyBox.Clear();ReplaceKeyBox.IsChecked=true;
   HostBox.Clear();CertificateBox.Clear();HostnameBox.Clear();ServerNameBox.Clear();VerifyNameBox.Clear();
   Feedback.Text="В загруженном наборе нет этого транспорта. Импортируйте отдельный профиль или выберите транспорт из набора.";
  }
 }
 void RememberSavedProfile(SettingsChange change){
  var saved=change.Settings;
  var profile=DownloadedTransportBox.Items.OfType<EngineSettings>().FirstOrDefault(p=>p.Transport==saved.Transport);
  if(profile==null)return;
  profile.Name=saved.Name;profile.Endpoint=saved.Endpoint;profile.Hostname=saved.Hostname;
  profile.Certificate=saved.Certificate;profile.CA=saved.CA;profile.ServerName=saved.ServerName;profile.VerifyName=saved.VerifyName;
  if(change.ReplaceKey)profile.Key=saved.Key;
 }
}
