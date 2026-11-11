package com.wang4386.cdtmonitor.widget

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver

class CdtWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = CdtWidget()

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        // Drop saved selections for widget ids that no longer exist.
        val activeIds = AppWidgetManager.getInstance(context)
            .getAppWidgetIds(ComponentName(context, CdtWidgetReceiver::class.java))
            .toSet()
        AppPreferences.pruneStaleWidgets(context, activeIds)
        RefreshWorker.enqueuePeriodic(context)
        RefreshWorker.enqueueNow(context)
    }
}
