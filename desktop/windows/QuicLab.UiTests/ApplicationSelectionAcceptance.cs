using System.IO;

using System.Windows;

using System.Windows.Controls;

using QuicLab.Windows;

partial class UiTests{

 static async Task ApplicationSelectionAcceptance(){

  var dir=Path.Combine(Path.GetTempPath(),"quic-app-ui-"+Guid.NewGuid());var file=Path.Combine(dir,"draft.json");

  ApplicationSelectionWindow? w=null;

  try{

   w=new(file);w.Show();await w.InitializedDraft;

   Check(((TextBlock)w.FindName("WarningText")).Text.Contains("не применяются"),"draft warning");

   ((TextBox)w.FindName("PathText")).Text=Environment.ProcessPath!;

   ((Button)w.FindName("AddButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));

   Check(((ListBox)w.FindName("SelectedList")).Items.Count==1,"exe added");

   ((ComboBox)w.FindName("ModeChoice")).SelectedIndex=1;

   Check(((ListBox)w.FindName("SelectedList")).Items.Count==0,"separate exclusion list");

   ((ComboBox)w.FindName("ModeChoice")).SelectedIndex=0;

   Check(((ListBox)w.FindName("SelectedList")).Items.Count==1,"include retained");

   ((Button)w.FindName("SaveButton")).RaiseEvent(new RoutedEventArgs(Button.ClickEvent));

   var until=DateTime.UtcNow.AddSeconds(5);while(!((TextBlock)w.FindName("ResultText")).Text.StartsWith("Черновик сохранён")&&DateTime.UtcNow<until)await Task.Delay(20);

   Check(File.Exists(file),"draft saved");w.Close();w=new(file);w.Show();await w.InitializedDraft;

   Check(((ListBox)w.FindName("SelectedList")).Items.Count==1,"reopen persisted selection");
   var preview=Environment.GetEnvironmentVariable("QUIC_APP_UI_PREVIEW");
   if(!string.IsNullOrEmpty(preview)){
    w.UpdateLayout();var bitmap=new System.Windows.Media.Imaging.RenderTargetBitmap((int)w.ActualWidth,(int)w.ActualHeight,96,96,System.Windows.Media.PixelFormats.Pbgra32);bitmap.Render(w);
    var encoder=new System.Windows.Media.Imaging.PngBitmapEncoder();encoder.Frames.Add(System.Windows.Media.Imaging.BitmapFrame.Create(bitmap));using var image=File.Create(preview);encoder.Save(image);
   }

  }finally{w?.Close();if(Directory.Exists(dir))Directory.Delete(dir,true);}

  Console.WriteLine("Application selection WPF draft PASS");

 }

}
