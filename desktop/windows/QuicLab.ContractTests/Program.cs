using System.Text.Json;
using QuicLab.Windows;
static void Check(bool ok, string message) { if (!ok) throw new Exception(message); }
var raw = await File.ReadAllTextAsync(args[0]); using var fixture = JsonDocument.Parse(raw);
Check(fixture.RootElement.GetProperty("version").GetInt32() == 1, "fixture version");
var config = fixture.RootElement.GetProperty("config").Clone();
using (var stream = new MemoryStream()) { await Frames.WriteAsync(stream, new { version = 1, id = "a", command = "status" }, default); stream.Position = 0; using var doc = await Frames.ReadAsync(stream, default); Check(doc.RootElement.GetProperty("id").GetString() == "a", "frame roundtrip"); }
using (var stream = new MemoryStream(new byte[] { 1, 0, 16, 0 })) { try { await Frames.ReadAsync(stream, default); throw new Exception("oversize accepted"); } catch (InvalidDataException) { } }
if (args.Contains("--expect-unavailable"))
{
    try { await new PipeClient().SendAsync("status"); throw new Exception("removed service accepted"); }
    catch (IOException) { Console.WriteLine("Removed SCM service fails closed: PASS"); return; }
}
if (args.Length > 1) { bool service = args[1] == "--service"; var pipe = service ? "quic-lab-windows-dev-v1" : args[1]; uint? pid = service ? null : uint.Parse(args[2]); var client = new PipeClient(pipe, pid); var r = await client.SendAsync("status"); Check(r.Ok, "native status"); if (args.Contains("--resume")) { r = await client.SendAsync("start"); Check(r.Ok && r.State == "running", "persisted config after SCM restart"); await client.SendAsync("stop"); } r = await client.SendAsync("apply_config", config); Check(r.Ok, "apply"); r = await client.SendAsync("start"); Check(r.Ok && r.State == "running", "start"); var second = new PipeClient(pipe, pid); r = await second.SendAsync("status"); Check(r.Ok && r.State == "running", "UI reconnect stopped backend"); r = await second.SendAsync("stop"); Check(r.Ok && r.State == "stopped", "stop"); try { await new PipeClient(pipe, 0).SendAsync("status"); throw new Exception("untrusted server accepted"); } catch (UnauthorizedAccessException) { } }
if (args.Length > 1 && args[1] == "--service") { var client = new PipeClient(); var restart = await client.SendAsync("start"); Check(restart.Ok, "restart persisted config"); var stop = await client.SendAsync("stop"); Check(stop.Ok, "cleanup"); }
Console.WriteLine("Contract/native IPC tests PASS");
// Dashboard contracts: traffic is real only when a fresh aggregate sample is present.
var now = DateTimeOffset.UtcNow;
var live = new Reply(true, "running", "", 1, new RuntimeInfo("vpn", "quic", "192.0.2.1:443", 1, new TrafficInfo(true, "one", now, 2048, 4096, 1024, 2048)));
var display = TrafficDisplay.Create(live, now);
Check(display.TxTotal == "2 КиБ" && display.TxRate == "1 КиБ/с", "traffic units");
Check(TrafficDisplay.Create(live, now.AddSeconds(10)).TxRate == "—", "stale samples must not look live");
Check(TrafficDisplay.Create(live with { State = "stopped" }, now).TxRate == "0 Б/с", "stopped speed");
Check(TrafficDisplay.Create(new Reply(true, "running", "", 1), now).TxTotal == "—", "legacy counters unavailable");
Console.WriteLine("Dashboard traffic contracts PASS");
