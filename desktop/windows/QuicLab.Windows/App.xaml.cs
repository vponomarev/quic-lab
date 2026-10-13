using System.Windows;
namespace QuicLab.Windows;

public partial class App : Application { protected override void OnStartup(StartupEventArgs e) { base.OnStartup(e); uint? pid = null; string pipe = "quic-lab-windows-dev-v1"; if (e.Args.Length == 3 && e.Args[0] == "--fixture") { pipe = e.Args[1]; pid = uint.Parse(e.Args[2]); } new MainWindow(new PipeClient(pipe, pid)).Show(); } }
