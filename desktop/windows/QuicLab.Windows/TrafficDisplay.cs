using System.Globalization;
using System.Text.Json.Serialization;
namespace QuicLab.Windows;

public record TrafficInfo(bool Available, string Session, [property: JsonPropertyName("observed_at")] DateTimeOffset ObservedAt,
 [property: JsonPropertyName("tx_bytes")] ulong TxBytes, [property: JsonPropertyName("rx_bytes")] ulong RxBytes,
 [property: JsonPropertyName("tx_bytes_per_second")] double TxRate, [property: JsonPropertyName("rx_bytes_per_second")] double RxRate);
public record RuntimeInfo(string Mode, string Transport, string Endpoint, [property: JsonPropertyName("route_count")] int RouteCount, TrafficInfo? Traffic, string[]? Targets = null, string? Name = null);
public record TrafficDisplay(string TxTotal, string RxTotal, string TxRate, string RxRate, string Hint)
{
    public static TrafficDisplay Create(Reply reply, DateTimeOffset now)
    {
        var t = reply.Runtime?.Traffic;
        if (t is null || !t.Available) return new("—", "—", "—", "—", "Счётчики ещё не получены");
        bool stopped = reply.State is "stopped" or "failed";
        bool fresh = now - t.ObservedAt <= TimeSpan.FromSeconds(4) && t.ObservedAt <= now.AddSeconds(2);
        return new(Bytes(t.TxBytes), Bytes(t.RxBytes), stopped ? "0 Б/с" : fresh ? Bytes(t.TxRate) + "/с" : "—", stopped ? "0 Б/с" : fresh ? Bytes(t.RxRate) + "/с" : "—", stopped ? "Последний замер подключения" : fresh ? "Трафик за текущее подключение" : "Скорость не обновляется · данные устарели");
    }
    public static string Bytes(double value)
    {
        if (!double.IsFinite(value) || value < 0) return "—";
        string[] units = { "Б", "КиБ", "МиБ", "ГиБ", "ТиБ" }; int unit = 0;
        while (value >= 1024 && unit < units.Length - 1) { value /= 1024; unit++; }
        return value.ToString("0.#", CultureInfo.GetCultureInfo("ru-RU")) + " " + units[unit];
    }
}
