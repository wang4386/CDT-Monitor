package com.wang4386.cdtmonitor.widget

import org.json.JSONObject
import java.io.BufferedReader
import java.io.IOException
import java.io.InputStream
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.nio.charset.StandardCharsets
import java.util.Locale

/**
 * Read-only client for the CDT Monitor API.
 *
 * All endpoints used here accept an API key with only the `widget:read`
 * scope: the compact widget summary, the full status (instances with IPs,
 * billing and schedules) and the per-instance traffic history.
 */
object CdtApi {

    @Throws(IOException::class)
    fun fetchSummary(siteUrl: String, apiKey: String): Summary =
        Summary.fromJson(getJson(siteUrl, apiKey, "/api/v1/widget/summary"))

    @Throws(IOException::class)
    fun fetchStatus(siteUrl: String, apiKey: String): StatusResponse =
        parseStatus(getJson(siteUrl, apiKey, "/api/v1/status"))

    /** Parsed status plus the raw payload, for callers that cache it. */
    @Throws(IOException::class)
    fun fetchStatusWithRaw(siteUrl: String, apiKey: String): Pair<StatusResponse, String> {
        val root = getJson(siteUrl, apiKey, "/api/v1/status")
        return parseStatus(root) to root.toString()
    }

    /** Daily traffic points for one instance, oldest first. */
    @Throws(IOException::class)
    fun fetchDailyHistory(siteUrl: String, apiKey: String, accountId: Long): List<HistoryPoint> {
        val root = getJson(siteUrl, apiKey, "/api/v1/accounts/$accountId/history")
        val daily = root.optJSONArray("daily") ?: return emptyList()
        val points = ArrayList<HistoryPoint>(daily.length())
        for (index in 0 until daily.length()) {
            val raw = daily.getJSONObject(index)
            points += HistoryPoint(
                at = raw.optString("at", ""),
                traffic = raw.optDouble("traffic", 0.0),
            )
        }
        return points
    }

    /** Result of an asynchronous server job (control / refresh actions). */
    data class JobState(val status: String, val error: String) {
        val isDone: Boolean get() = status == "completed" || status == "failed"
        val isSuccess: Boolean get() = status == "completed"
    }

    /**
     * Enqueue an instance power action ("start" / "stop"). Requires an API
     * key with the instance:control scope. Returns the job id.
     */
    @Throws(IOException::class)
    fun controlAction(siteUrl: String, apiKey: String, accountId: Long, action: String): String {
        val root = postJson(siteUrl, apiKey, "/api/v1/accounts/$accountId/actions/$action")
        return root.optString("id", "")
    }

    /** Enqueue a data refresh for one instance (instance:control). */
    @Throws(IOException::class)
    fun refreshAccount(siteUrl: String, apiKey: String, accountId: Long): String {
        val root = postJson(siteUrl, apiKey, "/api/v1/accounts/$accountId/refresh")
        return root.optString("id", "")
    }

    /** Poll a job created by a control / refresh call. */
    @Throws(IOException::class)
    fun fetchJob(siteUrl: String, apiKey: String, jobId: String): JobState {
        val root = getJson(siteUrl, apiKey, "/api/v1/jobs/$jobId")
        return JobState(
            status = root.optString("status", ""),
            error = root.optString("error", ""),
        )
    }

    @Throws(IOException::class)
    private fun postJson(siteUrl: String, apiKey: String, path: String): JSONObject {
        val baseUrl = normalizeBaseUrl(siteUrl)
        if (baseUrl.isEmpty() || apiKey.isBlank()) {
            throw IOException("请先填写站点地址和 API Key")
        }
        var connection: HttpURLConnection? = null
        try {
            connection = (URL("$baseUrl$path").openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 5_000
                readTimeout = 10_000
                doOutput = true
                setRequestProperty("Accept", "application/json")
                setRequestProperty("Authorization", "Bearer ${apiKey.trim()}")
                outputStream.use { it.write(ByteArray(0)) }
            }
            val status = connection.responseCode
            val body = readBody(if (status >= 400) connection.errorStream else connection.inputStream)
            if (status !in 200..299) {
                throw IOException(errorMessage(body, status))
            }
            if (body.isBlank()) return JSONObject()
            return try {
                JSONObject(body)
            } catch (e: org.json.JSONException) {
                throw IOException("服务返回的数据格式无效", e)
            }
        } finally {
            connection?.disconnect()
        }
    }

    @Throws(IOException::class)
    private fun getJson(siteUrl: String, apiKey: String, path: String): JSONObject {
        val baseUrl = normalizeBaseUrl(siteUrl)
        if (baseUrl.isEmpty() || apiKey.isBlank()) {
            throw IOException("请先填写站点地址和 API Key")
        }
        var connection: HttpURLConnection? = null
        try {
            connection = (URL("$baseUrl$path").openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                connectTimeout = 5_000
                readTimeout = 8_000
                setRequestProperty("Accept", "application/json")
                setRequestProperty("Authorization", "Bearer ${apiKey.trim()}")
            }
            val status = connection.responseCode
            val body = readBody(if (status >= 400) connection.errorStream else connection.inputStream)
            if (status !in 200..299) {
                throw IOException(errorMessage(body, status))
            }
            return try {
                JSONObject(body)
            } catch (e: org.json.JSONException) {
                throw IOException("服务返回的数据格式无效", e)
            }
        } finally {
            connection?.disconnect()
        }
    }

    /**
     * Accepts only plain http(s) origins (optionally with a path prefix):
     * no credentials, query or fragment. Implemented with java.net.URI so it
     * stays unit-testable on the JVM.
     */
    fun normalizeBaseUrl(value: String?): String {
        if (value.isNullOrBlank()) return ""
        val candidate = value.trim().trimEnd('/')
        val uri = try {
            URI(candidate)
        } catch (e: Exception) {
            return ""
        }
        val schemeOk = uri.scheme.equals("http", ignoreCase = true) ||
            uri.scheme.equals("https", ignoreCase = true)
        return if (schemeOk && !uri.host.isNullOrEmpty() &&
            uri.userInfo == null && uri.query == null && uri.fragment == null
        ) {
            candidate
        } else {
            ""
        }
    }

    @Throws(IOException::class)
    private fun readBody(stream: InputStream?): String {
        if (stream == null) return ""
        return BufferedReader(InputStreamReader(stream, StandardCharsets.UTF_8)).use { reader ->
            buildString {
                while (true) {
                    val line = reader.readLine() ?: break
                    append(line)
                }
            }
        }
    }

    private fun errorMessage(body: String, status: Int): String {
        try {
            val error = JSONObject(body).optJSONObject("error")
            if (error != null && error.optString("message").isNotEmpty()) {
                return error.optString("message")
            }
        } catch (ignored: Exception) {
            // Keep the HTTP status when the server returns a non-JSON error page.
        }
        return when (status) {
            401 -> "API Key 无效或已撤销"
            403 -> "API Key 缺少所需权限（widget:read / instance:control）"
            else -> "请求失败（HTTP $status）"
        }
    }

    // ----- Widget summary (compact projection used by the Glance widget) -----

    data class Summary(
        val accounts: List<Account>,
        val updatedAt: String,
    ) {
        fun find(accountId: Long): Account? = accounts.firstOrNull { it.id == accountId }

        fun toJson(): JSONObject = JSONObject().apply {
            put("accounts", org.json.JSONArray().apply {
                accounts.forEach { put(it.toJson()) }
            })
            put("updated_at", updatedAt)
        }

        companion object {
            fun fromJson(root: JSONObject): Summary {
                val rawAccounts = root.optJSONArray("accounts")
                    ?: throw org.json.JSONException("accounts missing")
                val accounts = ArrayList<Account>(rawAccounts.length())
                for (index in 0 until rawAccounts.length()) {
                    val raw = rawAccounts.getJSONObject(index)
                    accounts += Account(
                        id = raw.optLong("id", -1L),
                        name = raw.optString("name", "CDT 实例"),
                        status = raw.optString("status", "Unknown"),
                        used = raw.optDouble("used", 0.0),
                        total = raw.optDouble("total", 0.0),
                        percentage = raw.optDouble("percentage", 0.0),
                        updatedAt = raw.optString("updated_at", ""),
                    )
                }
                return Summary(accounts, root.optString("updated_at", ""))
            }
        }
    }

    data class Account(
        val id: Long,
        val name: String,
        val status: String,
        val used: Double,
        val total: Double,
        val percentage: Double,
        val updatedAt: String,
    ) {
        fun displayName(): String = name.trim().ifEmpty { "CDT 实例 #$id" }

        fun displayStatus(): String = statusLabel(status)

        fun isRunning(): Boolean = status.equals("Running", ignoreCase = true)

        fun displayUsage(): String =
            String.format(Locale.getDefault(), "%.2f / %.2f GB · %.1f%%", used, total, percentage)

        fun toJson(): JSONObject = JSONObject().apply {
            put("id", id)
            put("name", name)
            put("status", status)
            put("used", used)
            put("total", total)
            put("percentage", percentage)
            put("updated_at", updatedAt)
        }
    }

    // ----- Full status (in-app dashboard) -----

    data class StatusResponse(
        val accounts: List<StatusAccount>,
        val systemLastRun: String,
    )

    fun parseStatus(root: JSONObject): StatusResponse {
        val rawAccounts = root.optJSONArray("accounts")
            ?: throw org.json.JSONException("accounts missing")
        val accounts = ArrayList<StatusAccount>(rawAccounts.length())
        for (index in 0 until rawAccounts.length()) {
            val raw = rawAccounts.getJSONObject(index)
            accounts += StatusAccount(
                id = raw.optLong("id", -1L),
                account = raw.optString("account", ""),
                remark = raw.optString("remark", ""),
                regionName = raw.optString("region_name", ""),
                region = raw.optString("region", ""),
                flowTotal = raw.optDouble("flow_total", 0.0),
                flowUsed = raw.optDouble("flow_used", 0.0),
                percentage = raw.optDouble("percentage", 0.0),
                threshold = raw.optInt("threshold", 0),
                overThreshold = raw.optBoolean("over_threshold", false),
                instanceStatus = raw.optString("instance_status", "Unknown"),
                publicIp = raw.optString("public_ip", ""),
                publicIpv6 = raw.optString("public_ipv6", ""),
                monthlyCost = raw.optNullableDouble("monthly_cost"),
                balance = raw.optNullableDouble("balance"),
                currency = raw.optString("currency", ""),
                billingError = raw.optString("billing_error", ""),
                keepAlive = if (raw.isNull("keep_alive")) null else raw.optBoolean("keep_alive"),
                scheduleEnabled = raw.optBoolean("schedule_enabled", false),
                startTime = raw.optString("start_time", ""),
                stopTime = raw.optString("stop_time", ""),
                lastUpdated = raw.optString("last_updated", ""),
                stale = raw.optBoolean("stale", false),
            )
        }
        return StatusResponse(accounts, root.optString("system_last_run", ""))
    }

    data class StatusAccount(
        val id: Long,
        val account: String,
        val remark: String,
        val regionName: String,
        val region: String,
        val flowTotal: Double,
        val flowUsed: Double,
        val percentage: Double,
        val threshold: Int,
        val overThreshold: Boolean,
        val instanceStatus: String,
        val publicIp: String,
        val publicIpv6: String,
        val monthlyCost: Double?,
        val balance: Double?,
        val currency: String,
        val billingError: String,
        val keepAlive: Boolean?,
        val scheduleEnabled: Boolean,
        val startTime: String,
        val stopTime: String,
        val lastUpdated: String,
        val stale: Boolean,
    ) {
        fun displayName(): String =
            remark.trim().ifEmpty { account.trim() }.ifEmpty { "CDT 实例 #$id" }

        fun displayRegion(): String = regionName.trim().ifEmpty { region.trim() }

        fun displayStatus(): String = statusLabel(instanceStatus)

        fun isRunning(): Boolean = instanceStatus.equals("Running", ignoreCase = true)

        fun displayMoney(value: Double?): String {
            if (value == null) return "—"
            val symbol = when {
                currency.equals("CNY", ignoreCase = true) || currency.isEmpty() -> "¥"
                currency.equals("USD", ignoreCase = true) -> "$"
                else -> "$currency "
            }
            return symbol + String.format(Locale.getDefault(), "%.2f", value)
        }
    }

    data class HistoryPoint(
        val at: String,
        val traffic: Double,
    )

    private fun JSONObject.optNullableDouble(name: String): Double? =
        if (isNull(name)) null else optDouble(name, Double.NaN).takeUnless { it.isNaN() }
}

private fun statusLabel(status: String): String = when {
    status.equals("Running", ignoreCase = true) -> "运行中"
    status.equals("Stopped", ignoreCase = true) -> "已停止"
    status.equals("Starting", ignoreCase = true) -> "启动中"
    status.equals("Stopping", ignoreCase = true) -> "停止中"
    else -> "未知"
}
