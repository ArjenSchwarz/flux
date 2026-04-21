import Foundation

/// UserDefaults keys for the widget-extension → main-app diagnostic heartbeat.
/// Kept in one place so a typo can't silently disconnect writer from reader.
public enum WidgetDiagnosticKeys {
    public static let lastRunAt = "widgetLastRunAt"
    public static let canReadGroup = "widgetCanReadGroup"
    public static let cacheReadable = "widgetCacheReadable"
    public static let tokenReadable = "widgetTokenReadable"
    public static let apiURL = "widgetAPIURL"
    public static let lastSource = "widgetLastSource"
}
