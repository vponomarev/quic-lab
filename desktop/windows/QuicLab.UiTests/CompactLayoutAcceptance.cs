using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using QuicLab.Windows;
partial class UiTests
{
    static async Task CompactLayoutAcceptance()
    {
        var window = new MainWindow(new FakeClient()); window.Show(); await Ready(window);
        foreach (var size in new[] { (440d,570d), (390d,470d) }) {
            window.Width=size.Item1; window.Height=size.Item2; await Task.Delay(100); window.UpdateLayout();
            var scroll=FindScroll(window)!;
            Check(scroll.ScrollableHeight < 1, "dashboard must fit without scrolling at " + size + " overflow=" + scroll.ScrollableHeight);
            foreach(var name in new[]{"ToggleButton","TxTotalText","RxTotalText","TxRateText","RxRateText","SettingsPanel"}) {
                var element=(FrameworkElement)window.FindName(name);
                var bounds=element.TransformToAncestor(scroll).TransformBounds(new Rect(element.RenderSize));
                Check(bounds.Bottom <= scroll.ViewportHeight+1,"off-screen control: "+name);
            }
        }
        window.Close(); Console.WriteLine("Compact layout at default and minimum size PASS");
    }
    static ScrollViewer? FindScroll(DependencyObject root) {
        if(root is ScrollViewer scroll)return scroll;
        for(int i=0;i<VisualTreeHelper.GetChildrenCount(root);i++) {
            var result=FindScroll(VisualTreeHelper.GetChild(root,i));if(result!=null)return result;
        }
        return null;
    }
}
