using System.Text.Json.Serialization;
namespace QuicLab.Windows;

public sealed class AppRoutingPolicy {
 public int Version {get;set;}=2;
 public bool Enabled {get;set;}
 public string Mode {get;set;}=ApplicationSelection.OnlyMode;
 public string[] Only {get;set;}=[];
 public string[] Except {get;set;}=[];
 public string Unknown {get;set;}="tunnel";
}
public sealed class AppRoutingStatistics {
 public bool Enabled {get;set;}
 [JsonPropertyName("direct_tx_bytes")]public ulong DirectTxBytes {get;set;}
 [JsonPropertyName("direct_rx_bytes")]public ulong DirectRxBytes {get;set;}
 public ulong Resolved {get;set;} public ulong Unknown {get;set;} public ulong Shared {get;set;} public ulong Blocked {get;set;}
 public ulong Overloaded {get;set;} [JsonPropertyName("timed_out")]public ulong TimedOut {get;set;}
}
