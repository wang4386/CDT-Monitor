package com.wang4386.cdtmonitor.widget

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CdtApiTest {

    private val sampleJson = """
        {
          "accounts": [
            {"id": 7, "name": "东京节点", "status": "Running", "used": 12.4, "total": 50.0,
             "percentage": 24.8, "updated_at": "2026-11-11T05:30:00Z"},
            {"id": 9, "name": "", "status": "Stopped", "used": 49.5, "total": 50.0,
             "percentage": 99.0, "updated_at": "2026-11-11T05:30:00Z"}
          ],
          "updated_at": "2026-11-11T05:30:00Z"
        }
    """.trimIndent()

    @Test
    fun summary_parsesAccounts() {
        val summary = CdtApi.Summary.fromJson(JSONObject(sampleJson))
        assertEquals(2, summary.accounts.size)
        assertEquals("2026-11-11T05:30:00Z", summary.updatedAt)
        val first = summary.find(7L)
        assertEquals("东京节点", first?.displayName())
        assertEquals("运行中", first?.displayStatus())
        assertTrue(first?.isRunning() == true)
        assertNull(summary.find(123L))
    }

    @Test
    fun account_fallbackNameAndStatus() {
        val summary = CdtApi.Summary.fromJson(JSONObject(sampleJson))
        val second = summary.find(9L)
        assertEquals("CDT 实例 #9", second?.displayName())
        assertEquals("已停止", second?.displayStatus())
    }

    @Test
    fun summary_roundTripsThroughJson() {
        val summary = CdtApi.Summary.fromJson(JSONObject(sampleJson))
        val restored = CdtApi.Summary.fromJson(summary.toJson())
        assertEquals(summary, restored)
    }

    @Test
    fun status_parsesFullAccountFields() {
        val json = """
            {
              "accounts": [
                {"id": 3, "account": "acc-1", "remark": "HK CDT", "region_name": "中国香港",
                 "flow_total": 200.0, "flow_used": 48.53, "percentage": 24.3, "threshold": 90,
                 "instance_status": "Running", "public_ip": "1.2.3.4",
                 "monthly_cost": 12.5, "balance": null, "currency": "CNY",
                 "schedule_enabled": true, "start_time": "07:00", "stop_time": "23:00",
                 "last_updated": "2026-11-11T05:46:00Z", "stale": false}
              ],
              "system_last_run": "2026-11-11T05:46:00Z"
            }
        """.trimIndent()
        val status = CdtApi.parseStatus(JSONObject(json))
        assertEquals(1, status.accounts.size)
        val account = status.accounts[0]
        assertEquals("HK CDT", account.displayName())
        assertEquals("中国香港", account.displayRegion())
        assertEquals("1.2.3.4", account.publicIp)
        assertEquals("¥12.50", account.displayMoney(account.monthlyCost))
        assertEquals("—", account.displayMoney(account.balance))
        assertTrue(account.scheduleEnabled)
        assertEquals("运行中", account.displayStatus())
    }

    @Test
    fun normalizeBaseUrl_acceptsPlainOrigins() {
        assertEquals("https://monitor.example.com", CdtApi.normalizeBaseUrl("https://monitor.example.com"))
        assertEquals("https://monitor.example.com", CdtApi.normalizeBaseUrl("  https://monitor.example.com/  "))
        assertEquals("http://192.168.1.8:8080", CdtApi.normalizeBaseUrl("http://192.168.1.8:8080"))
        assertEquals("https://example.com/cdt", CdtApi.normalizeBaseUrl("https://example.com/cdt"))
    }

    @Test
    fun normalizeBaseUrl_rejectsUnsafeInputs() {
        assertEquals("", CdtApi.normalizeBaseUrl(null))
        assertEquals("", CdtApi.normalizeBaseUrl(""))
        assertEquals("", CdtApi.normalizeBaseUrl("monitor.example.com"))
        assertEquals("", CdtApi.normalizeBaseUrl("ftp://monitor.example.com"))
        assertEquals("", CdtApi.normalizeBaseUrl("https://user:pass@monitor.example.com"))
        assertEquals("", CdtApi.normalizeBaseUrl("https://monitor.example.com/?token=1"))
        assertEquals("", CdtApi.normalizeBaseUrl("https://monitor.example.com/#frag"))
    }
}
