using System.Windows;
namespace QuicLab.Windows;

public partial class App : Application {
 protected override void OnStartup(StartupEventArgs e) {
  base.OnStartup(e);
  var client=e.Args.Length==3&&e.Args[0]=="--fixture"
   ? new PipeClient(e.Args[1],uint.Parse(e.Args[2])) : new PipeClient();
  new MainWindow(client).Show();
 }
}
