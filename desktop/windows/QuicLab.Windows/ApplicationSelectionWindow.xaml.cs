using System.Diagnostics;

using System.IO;

using System.Windows;

using System.Windows.Controls;

using Microsoft.Win32;

namespace QuicLab.Windows;

public partial class ApplicationSelectionWindow:Window{

 readonly string file;

 readonly TaskCompletionSource initialized=new(TaskCreationOptions.RunContinuationsAsynchronously);

 ApplicationSelection draft=new();bool ready,closed;

 public Task InitializedDraft=>initialized.Task;

 public ApplicationSelectionWindow(string? file=null){

  this.file=file??Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"QUIC Lab","application-selection-draft.json");

  InitializeComponent();SaveButton.IsEnabled=false;

  Loaded+=LoadDraft;Closed+=(_,_)=>closed=true;

 }

 async void LoadDraft(object sender,RoutedEventArgs args){

  try{draft=await ApplicationSelection.LoadAsync(file);if(closed)return;ModeChoice.SelectedIndex=draft.Mode==ApplicationSelection.OnlyMode?0:1;ready=true;SaveButton.IsEnabled=true;Render();}

  catch(Exception){if(!closed)ResultText.Text="Не удалось прочитать черновик. Исходный файл сохранён; перезапись отключена.";}

  finally{initialized.TrySetResult();}

 }

 void Render(){SelectedList.ItemsSource=draft.Current.ToArray();}

 void ModeChanged(object sender,SelectionChangedEventArgs e){if(!ready)return;draft.Mode=ModeChoice.SelectedIndex==0?ApplicationSelection.OnlyMode:ApplicationSelection.ExceptMode;Render();ResultText.Text="Изменения не сохранены. Сеть не меняется.";}

 void BrowseClick(object sender,RoutedEventArgs e){var dialog=new OpenFileDialog{Filter="Приложения Windows|*.exe",CheckFileExists=true};if(dialog.ShowDialog(this)==true)PathText.Text=dialog.FileName;}

 void AddClick(object sender,RoutedEventArgs e){

  if(!ready)return;

  try{if(!File.Exists(PathText.Text))throw new InvalidDataException();draft.Add(PathText.Text);Render();ResultText.Text="Изменения не сохранены. Сеть не меняется.";}

  catch(Exception){ResultText.Text="Выберите доступный локальный файл .exe по полному пути.";}

 }

 void RemoveClick(object sender,RoutedEventArgs e){if(!ready)return;foreach(string path in SelectedList.SelectedItems.Cast<string>().ToArray())draft.Remove(path);Render();ResultText.Text="Изменения не сохранены. Сеть не меняется.";}

 async void SaveClick(object sender,RoutedEventArgs e){

  if(!ready)return;EditorRoot.IsEnabled=false;

  try{await draft.SaveAsync(file);if(!closed)ResultText.Text="Черновик сохранён. Правила к сети не применяются.";}

  catch(Exception){if(!closed)ResultText.Text="Не удалось сохранить черновик. Повторите попытку.";}

  finally{if(!closed)EditorRoot.IsEnabled=true;}

 }

 async void RunningClick(object sender,RoutedEventArgs e){

  RunningButton.IsEnabled=false;ResultText.Text="Читаю список запущенных приложений…";

  try{

   var result=await Task.Run(()=>{

    var paths=new SortedSet<string>(StringComparer.OrdinalIgnoreCase);int skipped=0;

    foreach(var process in Process.GetProcesses())using(process){try{var path=process.MainModule?.FileName;if(path is not null&&Path.GetExtension(path).Equals(".exe",StringComparison.OrdinalIgnoreCase))paths.Add(path);else skipped++;}catch{skipped++;}}

    return (Paths:paths.ToArray(),Skipped:skipped);

   });

   if(closed)return;RunningChoice.ItemsSource=result.Paths;RunningChoice.Visibility=Visibility.Visible;

   ResultText.Text=$"Доступно: {result.Paths.Length}. Недоступно или завершилось: {result.Skipped}. Выберите путь и нажмите «Добавить».";

  }catch(Exception){if(!closed)ResultText.Text="Не удалось прочитать список процессов. Используйте «Обзор».";}

  finally{if(!closed)RunningButton.IsEnabled=true;}

 }

 void RunningChanged(object sender,SelectionChangedEventArgs e){if(RunningChoice.SelectedItem is string path)PathText.Text=path;}

}
