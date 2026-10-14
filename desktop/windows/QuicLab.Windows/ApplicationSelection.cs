using System.IO;
using System.Text;
using System.Text.Json;
namespace QuicLab.Windows;

// UI draft only. The privileged service must resolve WFP identities before applying any rule.
public sealed class ApplicationSelection
{
    public const string OnlyMode="only_selected", ExceptMode="all_except_selected";
    const int Maximum=1024*1024;
    string mode=OnlyMode;
    readonly List<string> only=[], except=[];
    public string Mode {get=>mode;set{if(value!=OnlyMode&&value!=ExceptMode)throw new InvalidDataException();mode=value;}}
    List<string> Active=>mode==OnlyMode?only:except;
    public IReadOnlyList<string> Current=>Active.AsReadOnly();
    static string ValidatePath(string path){
        if(string.IsNullOrWhiteSpace(path)||path.Length>32760||path.Length<3||!char.IsAsciiLetter(path[0])||path[1]!=':'||path[2]!='\\'||path[2..].Contains(':')||path.IndexOfAny(Path.GetInvalidPathChars())>=0||!Path.GetExtension(path).Equals(".exe",StringComparison.OrdinalIgnoreCase))throw new InvalidDataException();
        return Path.GetFullPath(path);
    }
    public void Add(string path){path=ValidatePath(path);if(Active.Contains(path,StringComparer.OrdinalIgnoreCase))return;if(Active.Count>=1024)throw new InvalidDataException();Active.Add(path);}
    public void Remove(string path)=>Active.RemoveAll(p=>p.Equals(path,StringComparison.OrdinalIgnoreCase));
    public string Serialize()=>JsonSerializer.Serialize(new{version=1,mode,only,except});
    public static ApplicationSelection Parse(string raw){
        try{
            if(Encoding.UTF8.GetByteCount(raw)>Maximum)throw new InvalidDataException();
            using var doc=JsonDocument.Parse(raw);var root=doc.RootElement;
            if(root.ValueKind!=JsonValueKind.Object)throw new InvalidDataException();
            var seen=new HashSet<string>();foreach(var prop in root.EnumerateObject())if(!seen.Add(prop.Name)||prop.Name is not ("version" or "mode" or "only" or "except"))throw new InvalidDataException();
            if(seen.Count!=4||root.GetProperty("version").GetInt32()!=1)throw new InvalidDataException();
            var result=new ApplicationSelection();
            foreach(var key in new[]{"only","except"}){
                result.Mode=key=="only"?OnlyMode:ExceptMode;var list=root.GetProperty(key);
                if(list.ValueKind!=JsonValueKind.Array||list.GetArrayLength()>1024)throw new InvalidDataException();
                foreach(var value in list.EnumerateArray()){
                    var path=value.GetString()??throw new InvalidDataException();var count=result.Current.Count;result.Add(path);
                    if(result.Current.Count==count)throw new InvalidDataException();
                }
            }
            result.Mode=root.GetProperty("mode").GetString()??"";return result;
        }catch(Exception e)when(e is JsonException or InvalidOperationException or FormatException or ArgumentException or KeyNotFoundException or OverflowException){throw new InvalidDataException("Invalid application selection",e);}
    }
    public static async Task<ApplicationSelection> LoadAsync(string file){
        try{
            await using var stream=new FileStream(file,FileMode.Open,FileAccess.Read,FileShare.Read,4096,true);
            if(stream.Length>Maximum)throw new InvalidDataException();
            using var data=new MemoryStream();var buffer=new byte[4096];int n;
            while((n=await stream.ReadAsync(buffer))!=0){if(data.Length+n>Maximum)throw new InvalidDataException();data.Write(buffer,0,n);}
            return Parse(new UTF8Encoding(false,true).GetString(data.ToArray()));
        }catch(FileNotFoundException){return new();}catch(DirectoryNotFoundException){return new();}
        catch(DecoderFallbackException e){throw new InvalidDataException("Invalid UTF-8",e);}
    }
    public async Task SaveAsync(string file){
        var raw=Serialize();if(Encoding.UTF8.GetByteCount(raw)>Maximum)throw new InvalidDataException();
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(file))!);
        var temporary=file+"."+Guid.NewGuid().ToString("N")+".tmp";
        try{await File.WriteAllTextAsync(temporary,raw,new UTF8Encoding(false));File.Move(temporary,file,true);}
        finally{if(File.Exists(temporary))File.Delete(temporary);}
    }
}
