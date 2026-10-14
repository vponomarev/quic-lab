using System.Buffers.Binary;
using System.IO;
using System.IO.Pipes;
using System.Runtime.InteropServices;
using System.Security.Principal;
using System.Text.Json;
using Microsoft.Win32.SafeHandles;
namespace QuicLab.Windows;

public static class Frames
{
    public const int Maximum = 1048576;
    public static async Task WriteAsync(Stream stream, object request, CancellationToken ct) { var data = JsonSerializer.SerializeToUtf8Bytes(request); if (data.Length > Maximum) throw new InvalidDataException("Message too large"); byte[] size = new byte[4]; BinaryPrimitives.WriteInt32LittleEndian(size, data.Length); await stream.WriteAsync(size, ct); await stream.WriteAsync(data, ct); await stream.FlushAsync(ct); }
    public static async Task<JsonDocument> ReadAsync(Stream stream, CancellationToken ct) { byte[] size = new byte[4]; await stream.ReadExactlyAsync(size, ct); int n = BinaryPrimitives.ReadInt32LittleEndian(size); if (n <= 0 || n > Maximum) throw new InvalidDataException("Invalid frame"); byte[] data = new byte[n]; await stream.ReadExactlyAsync(data, ct); return JsonDocument.Parse(data); }
}
public record Reply(bool Ok, string State, string Error, ulong Revision, RuntimeInfo? Runtime = null, string[]? Capabilities = null, ulong SavedRevision = 0);
public interface IStatusClient { Task<Reply> SendAsync(string command, JsonElement? config = null, CancellationToken cancellationToken = default); }
public sealed class PipeClient : IStatusClient, ISettingsClient
{
    const string Service = "QuicLabWindowsPreview";
    readonly string pipeName; readonly uint? fixturePid;
    public PipeClient(string pipeName = "quic-lab-windows-preview-v1", uint? fixturePid = null) { this.pipeName = pipeName; this.fixturePid = fixturePid; }
    public async Task<Reply> SendAsync(string command, JsonElement? config = null, CancellationToken cancellationToken = default) {
        using var doc=await SendDocument(command,config,null,cancellationToken);return ParseStatus(doc.RootElement);
    }
    public async Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default){
        using var doc=await SendDocument(command,null,change,cancellationToken);var r=doc.RootElement;
        var view=r.TryGetProperty("settings",out var v)?JsonSerializer.Deserialize<SettingsView>(v,SettingsDraft.Json):null;
        string field="";if(r.TryGetProperty("field_error",out var f)){field=f.GetProperty("field").GetString()??"";if(f.TryGetProperty("row",out var row))field+=" · строка "+row.GetInt32();field+=" · "+f.GetProperty("code").GetString();}
        return new(ParseStatus(r),view,field);
    }
    static Reply ParseStatus(JsonElement r){var status=r.GetProperty("status");return new(r.GetProperty("ok").GetBoolean(),status.GetProperty("state").GetString()??"unknown",r.TryGetProperty("error",out var error)?error.GetString()??"operation_failed":"",status.GetProperty("revision").GetUInt64(),status.TryGetProperty("runtime",out var runtime)?JsonSerializer.Deserialize<RuntimeInfo>(runtime,SettingsDraft.Json):null,r.TryGetProperty("capabilities",out var caps)?JsonSerializer.Deserialize<string[]>(caps):null,r.TryGetProperty("saved_revision",out var rev)?rev.GetUInt64():0);}
    async Task<JsonDocument> SendDocument(string command, JsonElement? config,SettingsChange? change,CancellationToken cancellationToken)
    {
        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken); deadline.CancelAfter(TimeSpan.FromSeconds(30)); var ct = deadline.Token;
        uint expected = fixturePid ?? ServicePid(); if (expected == 0) throw new UnauthorizedAccessException("No trusted service process");

        using var pipe = new NamedPipeClientStream(".", pipeName, PipeDirection.InOut, PipeOptions.Asynchronous, TokenImpersonationLevel.Identification);
        await pipe.ConnectAsync(3000, ct);
        if (!fixturePid.HasValue) { var owner = PipeOwner(pipe.SafePipeHandle); if (owner != "S-1-5-18" && owner != "S-1-5-32-544") throw new UnauthorizedAccessException("Service pipe has an unprivileged owner"); }
        if (!GetNamedPipeServerProcessId(pipe.SafePipeHandle, out var actual) || actual != expected || (!fixturePid.HasValue && ServicePid() != expected)) throw new UnauthorizedAccessException("Untrusted pipe server");
        string id = Guid.NewGuid().ToString("N"); var request = new Dictionary<string, object?> { { "version", 1 }, { "id", id }, { "command", command } }; if (config.HasValue) request["config"] = config.Value; if(change!=null)request["settings"]=JsonSerializer.SerializeToElement(change,SettingsDraft.Json);
        await Frames.WriteAsync(pipe, request, ct); var doc = await Frames.ReadAsync(pipe, ct); var r = doc.RootElement;
        if (r.GetProperty("version").GetInt32() != 1 || r.GetProperty("id").GetString() != id) {doc.Dispose();throw new InvalidDataException("Response mismatch");}
        return doc;
    }
    static uint ServicePid() { IntPtr manager = OpenSCManager(null, null, 1); if (manager == IntPtr.Zero) throw new IOException("Service unavailable"); try { IntPtr service = OpenService(manager, Service, 4); if (service == IntPtr.Zero) throw new IOException("Service not installed"); try { if (!QueryServiceStatusEx(service, 0, out var status, Marshal.SizeOf<ServiceStatus>(), out _) || status.State != 4) throw new IOException("Service not running"); return status.Pid; } finally { CloseServiceHandle(service); } } finally { CloseServiceHandle(manager); } }
    [StructLayout(LayoutKind.Sequential)] struct ServiceStatus { public uint Type, State, Controls, Win32Exit, ServiceExit, Checkpoint, WaitHint, Pid, Flags; }
    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr OpenSCManager(string? machine, string? database, uint access);
    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr OpenService(IntPtr manager, string name, uint access);
    [DllImport("advapi32.dll", SetLastError = true)] static extern bool QueryServiceStatusEx(IntPtr service, int level, out ServiceStatus status, int size, out int needed);
    [DllImport("advapi32.dll")] static extern bool CloseServiceHandle(IntPtr handle);
    [DllImport("kernel32.dll", SetLastError = true)] static extern bool GetNamedPipeServerProcessId(SafePipeHandle pipe, out uint pid);
    static string PipeOwner(SafePipeHandle pipe) { uint e = GetSecurityInfo(pipe, 6, 1, out var owner, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, out var sd); if (e != 0) throw new UnauthorizedAccessException("Cannot verify pipe owner"); try { return new SecurityIdentifier(owner).Value; } finally { LocalFree(sd); } }
    [DllImport("advapi32.dll", SetLastError = true)] static extern uint GetSecurityInfo(SafePipeHandle handle, int type, uint info, out IntPtr owner, IntPtr group, IntPtr dacl, IntPtr sacl, out IntPtr descriptor);
    [DllImport("kernel32.dll")] static extern IntPtr LocalFree(IntPtr handle);
}
