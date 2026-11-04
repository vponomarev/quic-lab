using System.Windows;
namespace QuicLab.Windows;

public partial class SettingsWindow {
 ServerControlConfig? serverControl;
 bool replaceServerControl;
 string? importedControlKey;
 ServerExitView? serverExitView;
 void LoadServerControl(EngineSettings settings,string[]? capabilities){
  serverControl=settings.ServerControl?.Clone();replaceServerControl=false;importedControlKey=null;serverExitView=null;
  ServerExitBox.ItemsSource=null;ApplyServerExitButton.IsEnabled=false;
  RefreshServerExitsButton.IsEnabled=serverControl!=null&&client is IServerExitClient&&capabilities?.Contains("server-exits-v1")==true;
  ServerExitState.Text=serverControl==null?"Для выбора выхода нужен профиль, полученный по приглашению сервера.":"Остановите VPN, сохраните изменения и обновите список выходов.";
 }
 async void RefreshServerExitsClick(object sender,RoutedEventArgs e)=>await RunServerExit(false);
 async void ApplyServerExitClick(object sender,RoutedEventArgs e)=>await RunServerExit(true);
 async Task RunServerExit(bool apply){
  if(busy||!loaded||client is not IServerExitClient exits)return;
  if(HasChanges){ServerExitState.Text="Сначала сохраните изменения профиля.";return;}
  var change=apply&&serverExitView!=null&&ServerExitBox.SelectedItem is ServerExitChoice choice?new ServerExitChange{ID=choice.ID,Revision=serverExitView.Revision}:null;
  if(apply&&change==null)return;
  Busy(true);ApplyServerExitButton.IsEnabled=false;
  try{
   var reply=await exits.ServerExits(new(){ExpectedRevision=revision,Change=change},lifetime.Token);
   serverExitView=null;
   if(!reply.Status.Ok||reply.View==null){ServerExitState.Text=reply.Status.Error switch{
    "stop_before_apply"=>"Остановите VPN перед выбором выхода сервера.",
    "revision_conflict"=>"Профиль изменился. Закройте настройки и откройте снова.",
    "exit_revision_conflict"=>"Администратор или другое окно изменили выбор. Обновите список.",
    "exit_auth_expired"=>"Ключ управления больше не действует. Получите новый профиль.",
    "exit_unsupported"=>"Сервер или профиль не поддерживают выбор выхода.",
    "exit_denied"=>"Выход больше не разрешён. Обновите список.",
    _=>"Сервер не подтвердил выбор. Он мог сохраниться; обновите список перед повторной попыткой."};return;}
   serverExitView=reply.View;
   var choices=new[]{new ServerExitChoice()}.Concat(reply.View.Choices.Where(c=>!c.Disabled)).ToArray();
   ServerExitBox.ItemsSource=choices;ServerExitBox.SelectedItem=choices.FirstOrDefault(c=>c.ID==reply.View.Selected);
   ServerExitState.Text=reply.View.Pending?"Применение не завершено; трафик заблокирован.":reply.View.Blocked?"Текущий выход заблокирован. Выберите разрешённый.":reply.View.Legacy?"Используется общий выход сервера.":$"Текущий выход: {reply.View.Effective}. Можно подключать VPN.";
   ApplyServerExitButton.IsEnabled=true;
  }catch(OperationCanceledException){}catch(Exception){serverExitView=null;ServerExitState.Text="Нет подтверждения. Обновите список перед повторной попыткой.";}
  finally{Busy(false);}
 }
}
