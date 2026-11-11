# App entry points referenced from the manifest / Glance runtime.
-keep class com.wang4386.cdtmonitor.widget.CdtWidget { *; }
-keep class com.wang4386.cdtmonitor.widget.CdtWidgetReceiver { *; }
-keep class com.wang4386.cdtmonitor.widget.RefreshWorker { *; }
-keep class * extends androidx.glance.appwidget.action.ActionCallback { *; }
-keep class com.wang4386.cdtmonitor.widget.RefreshAction { *; }
-keep class com.wang4386.cdtmonitor.widget.SwitchAccountAction { *; }
# Data classes are only (de)serialized by hand via org.json; keep members.
-keep class com.wang4386.cdtmonitor.widget.CdtApi** { *; }
