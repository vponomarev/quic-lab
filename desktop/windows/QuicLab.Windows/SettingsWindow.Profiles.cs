using System.Windows;
using System.Windows.Controls;
namespace QuicLab.Windows;

public partial class SettingsWindow {
 bool librarySupported;
 async Task LoadProfileLibrary(string[]? capabilities){
  librarySupported=capabilities?.Contains("profile-library-v1")==true&&client is IProfileLibraryClient;
  SaveNewProfileButton.IsEnabled=librarySupported&&revision>0;
  SavedProfilesList.IsEnabled=librarySupported;UpdateProfileButtons();
  if(!librarySupported){ProfileLibraryHint.Text="Для библиотеки профилей обновите службу. Обычные настройки доступны.";return;}
  ProfileLibraryHint.Text=revision==0?"Сначала сохраните первое подключение во вкладке «Подключение».":"Снимки параметров VPN хранятся в защищённом хранилище службы. Выбор сохраняет локальные маршруты, DNS, интерфейс и правила приложений.";
  await RefreshProfiles();
 }
 async Task RefreshProfiles(){
  if(client is not IProfileLibraryClient library||!librarySupported)return;
  string? selected=(SavedProfilesList.SelectedItem as ProfileSummary)?.ID;
  var r=await library.SendProfileAsync("list_profiles",cancellationToken:lifetime.Token);
  if(!r.Status.Ok||r.Library==null){ProfileLibraryHint.Text="Не удалось прочитать библиотеку. Откройте настройки снова.";return;}
  SavedProfilesList.ItemsSource=r.Library.Profiles;
  SavedProfilesList.SelectedItem=r.Library.Profiles.FirstOrDefault(p=>p.ID==selected);
  ProfileCount.Text=$"Сохранено: {r.Library.Profiles.Length} / 32";
  UpdateProfileButtons();
 }
 void UpdateProfileButtons(){
  if(ActivateProfileButton==null)return;
  bool selected=librarySupported&&SavedProfilesList.SelectedItem is ProfileSummary;
  ActivateProfileButton.IsEnabled=ReplaceProfileButton.IsEnabled=DeleteProfileButton.IsEnabled=selected;
 }
 void ProfileSelectionChanged(object sender,SelectionChangedEventArgs e)=>UpdateProfileButtons();
 async void SaveNewProfileClick(object sender,RoutedEventArgs e)=>await SaveLibraryProfile(false);
 async void ReplaceProfileClick(object sender,RoutedEventArgs e)=>await SaveLibraryProfile(true);
 async Task SaveLibraryProfile(bool replace){
  if(busy||!loaded||!librarySupported||client is not IProfileLibraryClient library)return;
  var selected=SavedProfilesList.SelectedItem as ProfileSummary;if(replace&&selected==null)return;
  var draft=Capture(false);if(LocalError(draft) is string error){Feedback.Text=error;return;}
  if(replace&&MessageBox.Show(this,$"Заменить сохранённый профиль «{selected!.Name}» параметрами редактора?","Библиотека",MessageBoxButton.OKCancel,MessageBoxImage.Question)!=MessageBoxResult.OK)return;
  bool dirty=HasChanges;Busy(true);
  try{
   var r=await library.SendProfileAsync("save_profile",new(){ID=replace?selected!.ID:"",ExpectedRevision=revision,Settings=draft.Settings,ReplaceKey=draft.ReplaceKey,ReplaceServerControl=draft.ReplaceServerControl},lifetime.Token);
   if(!r.Status.Ok){Feedback.Text=ProfileError(r);return;}
   // Status may already include another client's later commit. Retain our fence.
   revision=draft.ExpectedRevision+1;if(!dirty)original=SettingsDraft.Fingerprint(Capture(false));
   await RefreshProfiles();Feedback.Text="Профиль сохранён в библиотеке. Работающее подключение не изменилось.";
  }catch(OperationCanceledException) when(lifetime.IsCancellationRequested){}catch(Exception){Feedback.Text="Нет ответа службы. Результат неизвестен; откройте настройки снова.";}finally{Busy(false);}
 }
 async void ActivateProfileClick(object sender,RoutedEventArgs e){
  if(busy||!loaded||!librarySupported||client is not IProfileLibraryClient library||SavedProfilesList.SelectedItem is not ProfileSummary selected)return;
  if(HasChanges&&MessageBox.Show(this,"Выбрать профиль и заменить несохранённые правки редактора?","Выбор профиля",MessageBoxButton.OKCancel,MessageBoxImage.Question)!=MessageBoxResult.OK)return;
  Busy(true);try{
   var current=await client.SendSettingsAsync("read_settings",cancellationToken:lifetime.Token);
   if(!current.Status.Ok){Feedback.Text=Explain(current);return;}
   bool reconnect=current.Status.State!="stopped";
   if(reconnect&&MessageBox.Show(this,"Выбрать профиль и переподключить VPN? Открытые VPN- и direct-соединения будут прерваны.","Выбор профиля",MessageBoxButton.OKCancel,MessageBoxImage.Question)!=MessageBoxResult.OK)return;
   var r=await library.SendProfileAsync("activate_profile",new(){ID=selected.ID,ExpectedRevision=revision,Reconnect=reconnect},lifetime.Token);
   if(r.Status.Ok||r.Status.Error=="backend_start_failed")await LoadSettings();
   Feedback.Text=r.Status.Ok?"Профиль выбран. Локальная маршрутизация и правила приложений сохранены.":ProfileError(r);
  }catch(OperationCanceledException) when(lifetime.IsCancellationRequested){}catch(Exception){Feedback.Text="Нет ответа службы. Результат выбора неизвестен; откройте настройки снова.";}finally{Busy(false);}
 }
 async void DeleteProfileClick(object sender,RoutedEventArgs e){
  if(busy||!loaded||!librarySupported||client is not IProfileLibraryClient library||SavedProfilesList.SelectedItem is not ProfileSummary selected)return;
  if(MessageBox.Show(this,$"Удалить профиль «{selected.Name}» из библиотеки? Текущее подключение сохранится.","Удаление профиля",MessageBoxButton.OKCancel,MessageBoxImage.Question)!=MessageBoxResult.OK)return;
  bool dirty=HasChanges;var request=new ProfileChange{ID=selected.ID,ExpectedRevision=revision};Busy(true);try{
   var r=await library.SendProfileAsync("delete_profile",request,lifetime.Token);
   if(!r.Status.Ok){Feedback.Text=ProfileError(r);return;}
   revision=request.ExpectedRevision+1;if(!dirty)original=SettingsDraft.Fingerprint(Capture(false));await RefreshProfiles();Feedback.Text="Профиль удалён из библиотеки. Текущее подключение сохранено.";
  }catch(OperationCanceledException) when(lifetime.IsCancellationRequested){}catch(Exception){Feedback.Text="Результат удаления неизвестен; откройте настройки снова.";}finally{Busy(false);}
 }
 static string ProfileError(ProfileLibraryReply r)=>r.Status.Error switch {
  "profile_not_found"=>"Профиль уже удалён. Откройте настройки снова.",
  "profile_limit"=>"Лимит библиотеки: 32 профиля и 1 МиБ данных. Удалите ненужные профили.",
  "config_required"=>"Сначала сохраните первое подключение.",
  _=>Explain(new SettingsReply(r.Status,null,""))
 };
}
