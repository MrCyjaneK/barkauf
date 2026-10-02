.pragma library

function empty() {
    return {
        destination: "",
        amountSat: -1,
        rawNumber: "",
        unit: "",
        lightning: "",
        lnAddress: "",
        lnurl: "",
        ark: "",
        onchain: ""
    }
}

function formatBTC(sats) {
    sats = Math.round(Number(sats) || 0)
    if (sats <= 0)
        return "0"
    var whole = Math.floor(sats / 100000000)
    var frac = sats % 100000000
    if (frac === 0)
        return String(whole)
    var digits = ("00000000" + frac).slice(-8).replace(/0+$/, "")
    return whole + "." + digits
}

function group(n) {
    // A normal space is too wide, and the Sailfish font draws U+2009 at the
    // same width. A half-size space glyph is actually half as wide.
    return String(Math.round(Number(n) || 0)).replace(
            /\B(?=(\d{3})+(?!\d))/g,
            "<span style=\"font-size:50%\">&nbsp;</span>")
}

function formatUSD(sats, rate) {
    return formatFiat(sats, rate, "USD")
}

var fiatTable = {
    USD: { code: "USD", name: "U.S. Dollar", symbol: "$", decimals: 2 },
    EUR: { code: "EUR", name: "Euro", symbol: "€", decimals: 2 },
    GBP: { code: "GBP", name: "British Pound", symbol: "£", decimals: 2 },
    CAD: { code: "CAD", name: "Canadian Dollar", symbol: "CA$", decimals: 2 },
    CHF: { code: "CHF", name: "Swiss Franc", symbol: "CHF", decimals: 2 },
    AUD: { code: "AUD", name: "Australian Dollar", symbol: "A$", decimals: 2 },
    JPY: { code: "JPY", name: "Japanese Yen", symbol: "¥", decimals: 0 },
    BRL: { code: "BRL", name: "Brazilian Real", symbol: "R$", decimals: 2 },
    KRW: { code: "KRW", name: "South Korean Won", symbol: "₩", decimals: 0 },
    INR: { code: "INR", name: "Indian Rupee", symbol: "₹", decimals: 2 },
    MXN: { code: "MXN", name: "Mexican Peso", symbol: "MX$", decimals: 2 },
    SGD: { code: "SGD", name: "Singapore Dollar", symbol: "S$", decimals: 2 }
}

function fiatCodes() {
    return ["USD", "EUR", "GBP", "CAD", "CHF", "AUD", "JPY", "BRL", "KRW", "INR", "MXN", "SGD"]
}

function fiatInfo(code) {
    return fiatTable[code] || fiatTable.USD
}

function fiatDecimals(code) {
    return fiatInfo(code).decimals
}

function formatFiat(sats, rate, code) {
    if (!(rate > 0))
        return ""
    var info = fiatInfo(code)
    var value = (Number(sats) || 0) / 100000000 * rate
    return value.toFixed(info.decimals)
}

function formatMoney(sats, rate, code) {
    var body = formatFiat(sats, rate, code)
    if (!body.length)
        return ""
    var info = fiatInfo(code)
    var gap = /^[A-Z]+$/.test(info.symbol) ? " " : ""
    return info.symbol + gap + body
}

function satsFromBTC(text) {
    text = String(text || "").replace(/^\s+|\s+$/g, "")
    if (text === "" || text === ".")
        return 0
    var parts = text.split(".")
    var whole = parseInt(parts[0] || "0", 10)
    if (isNaN(whole) || whole < 0)
        return 0
    var frac = (parts[1] || "").replace(/[^0-9]/g, "")
    if (frac.length > 8)
        frac = frac.substring(0, 8)
    while (frac.length < 8)
        frac += "0"
    return whole * 100000000 + parseInt(frac || "0", 10)
}

function satsFromUSD(text, rate) {
    if (!(rate > 0))
        return 0
    var usd = parseFloat(text)
    if (isNaN(usd) || usd <= 0)
        return 0
    return Math.round(usd / rate * 100000000)
}

function bolt11Amount(invoice) {
    var s = String(invoice || "").toLowerCase()
    var prefixes = ["lnbcrt", "lntbs", "lnbc", "lntb"]
    var rest = ""
    for (var i = 0; i < prefixes.length; i++) {
        if (s.indexOf(prefixes[i]) === 0) {
            rest = s.substring(prefixes[i].length)
            break
        }
    }
    if (!rest)
        return -1
    var match = /^([0-9]+)([munp])?1/.exec(rest)
    if (!match)
        return -1
    var n = parseInt(match[1], 10)
    if (match[2] === "m")
        return n * 100000
    if (match[2] === "u")
        return n * 100
    if (match[2] === "n")
        return Math.round(n / 10)
    if (match[2] === "p")
        return Math.round(n / 10000)
    return n * 100000000
}

function satVb(rate) {
    var r = Number(rate)
    if (!(r > 0) || !isFinite(r))
        return ""
    var digits = r >= 100 ? 0 : 2
    var text = r.toFixed(digits)
    if (digits > 0)
        text = text.replace(/0+$/, "").replace(/\.$/, "")
    return text + " sat/vB"
}

function feeRate(fee, vsize) {
    var v = Number(vsize) || 0
    if (v <= 0)
        return ""
    return satVb(Number(fee) / v)
}

function isOnchain(text) {
    return /^(bc1|tb1|bcrt1)[a-z0-9]{10,}$/i.test(text)
            || /^[13][a-km-zA-HJ-NP-Z1-9]{24,}$/.test(text)
}

function queryMap(query) {
    var params = {}
    var parts = String(query || "").split("&")
    for (var i = 0; i < parts.length; i++) {
        if (!parts[i])
            continue
        var cut = parts[i].indexOf("=")
        var key = cut >= 0 ? parts[i].substring(0, cut) : parts[i]
        var value = cut >= 0 ? parts[i].substring(cut + 1) : ""
        try {
            key = decodeURIComponent(key).toLowerCase()
            value = decodeURIComponent(value.replace(/\+/g, " "))
        } catch (e) {
        }
        params[key] = value
    }
    return params
}

function parse(text) {
    text = String(text || "").replace(/^\s+|\s+$/g, "")
    var result = empty()
    if (!text)
        return result
    var lower = text.toLowerCase()
    if (lower.indexOf("lightning:") === 0) {
        var addressed = lightningAddress(text)
        if (addressed) {
            result.destination = addressed
            result.lnAddress = addressed
            return result
        }
        var prefixed = lnurlValue(text)
        if (prefixed) {
            result.destination = prefixed
            result.lnurl = prefixed
            return result
        }
        result.destination = text
        result.lightning = text.substring("lightning:".length)
        var invoiceSats = bolt11Amount(result.lightning)
        if (invoiceSats >= 0)
            result.amountSat = invoiceSats
        return result
    }
    if (lower.indexOf("bitcoin:") === 0) {
        result.destination = text
        var q = text.indexOf("?")
        var body = q >= 0 ? text.substring("bitcoin:".length, q) : text.substring("bitcoin:".length)
        var params = q >= 0 ? queryMap(text.substring(q + 1)) : {}
        result.lightning = params.lightning || ""
        result.lnurl = lnurlValue(params.lnurl || "")
        if (!result.lnurl && result.lightning) {
            var embedded = lnurlValue(result.lightning)
            if (embedded) {
                result.lnurl = embedded
                result.lightning = ""
            }
        }
        result.ark = params.ark || ""
        result.onchain = params.bc || params.tb || params.bcrt || ""
        if (!result.onchain && body)
            result.onchain = body
        if (params.amount)
            result.amountSat = satsFromBTC(params.amount)
        if (result.amountSat < 0 && result.lightning) {
            var fromInvoice = bolt11Amount(result.lightning)
            if (fromInvoice >= 0)
                result.amountSat = fromInvoice
        }
        return result
    }
    var link = lnurlValue(text)
    if (link) {
        result.destination = link
        result.lnurl = link
        return result
    }
    if (lower.indexOf("ln") === 0 && text.length > 20) {
        result.destination = text
        result.lightning = text
        var boltSats = bolt11Amount(text)
        if (boltSats >= 0)
            result.amountSat = boltSats
        return result
    }
    if (/^ark1/i.test(text)) {
        result.destination = text
        result.ark = text
        return result
    }
    if (isOnchain(text)) {
        result.destination = text
        result.onchain = text
        return result
    }
    var addressed = lightningAddress(text)
    if (addressed) {
        result.destination = addressed
        result.lnAddress = addressed
        return result
    }
    var sats = /^([0-9]+)\s*sats?$/i.exec(text)
    if (sats) {
        result.amountSat = parseInt(sats[1], 10)
        return result
    }
    var btc = /^([0-9]*\.?[0-9]+)\s*btc$/i.exec(text)
    if (btc) {
        result.amountSat = satsFromBTC(btc[1])
        return result
    }
    if (/^\$/.test(text) || /usd$/i.test(text)) {
        var usd = /([0-9]*\.?[0-9]+)/.exec(text)
        if (usd) {
            result.rawNumber = usd[1]
            result.unit = "usd"
        }
        return result
    }
    if (/^[0-9]*\.?[0-9]+$/.test(text)) {
        result.rawNumber = text
        return result
    }
    result.destination = text
    return result
}

function loadBtcFiat(code, callback) {
    var quote = fiatInfo(code).code
    var xhr = new XMLHttpRequest()
    xhr.onreadystatechange = function() {
        if (xhr.readyState !== XMLHttpRequest.DONE)
            return
        if (xhr.status !== 200) {
            callback(0)
            return
        }
        var next = 0
        try {
            var body = JSON.parse(xhr.responseText)
            var results = body && body.results ? body.results : {}
            for (var key in results) {
                var n = Number(results[key])
                if (n > 0)
                    next = n
            }
        } catch (e) {
            next = 0
        }
        callback(next)
    }
    xhr.open("GET", "https://prices.cakewallet.com/v2/rates?base=BTC&quote=" + quote)
    xhr.send()
}

function loadBtcUsd(callback) {
    loadBtcFiat("USD", callback)
}

var bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
var lnurlFixedCache = {}
var lnurlWaiters = {}

function bech32Polymod(values) {
    var gen = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3]
    var chk = 1
    for (var i = 0; i < values.length; i++) {
        var top = chk >>> 25
        chk = (((chk & 0x1ffffff) << 5) ^ values[i]) >>> 0
        for (var j = 0; j < 5; j++) {
            if ((top >>> j) & 1)
                chk = (chk ^ gen[j]) >>> 0
        }
    }
    return chk
}

function bech32HrpExpand(hrp) {
    var out = []
    for (var i = 0; i < hrp.length; i++)
        out.push(hrp.charCodeAt(i) >> 5)
    out.push(0)
    for (var n = 0; n < hrp.length; n++)
        out.push(hrp.charCodeAt(n) & 31)
    return out
}

function convertBits(data, fromBits, toBits, pad) {
    var acc = 0
    var bits = 0
    var maxv = (1 << toBits) - 1
    var maxAcc = (1 << (fromBits + toBits - 1)) - 1
    var out = []
    for (var i = 0; i < data.length; i++) {
        var value = data[i]
        if ((value >> fromBits) !== 0)
            return null
        acc = ((acc << fromBits) | value) & maxAcc
        bits += fromBits
        while (bits >= toBits) {
            bits -= toBits
            out.push((acc >> bits) & maxv)
        }
    }
    if (pad) {
        if (bits > 0)
            out.push((acc << (toBits - bits)) & maxv)
    } else if (bits >= fromBits || ((acc << (toBits - bits)) & maxv) !== 0) {
        return null
    }
    return out
}

function decodeLnurl(text) {
    var s = lnurlValue(text)
    if (!s)
        return ""
    s = s.toLowerCase()
    var pos = s.lastIndexOf("1")
    if (pos < 1 || pos + 7 > s.length)
        return ""
    var hrp = s.substring(0, pos)
    var payload = s.substring(pos + 1)
    var data = []
    for (var i = 0; i < payload.length; i++) {
        var n = bech32Charset.indexOf(payload.charAt(i))
        if (n < 0)
            return ""
        data.push(n)
    }
    var mixed = bech32HrpExpand(hrp).concat(data)
    if (bech32Polymod(mixed) !== 1 || hrp !== "lnurl")
        return ""
    var bytes = convertBits(data.slice(0, data.length - 6), 5, 8, false)
    if (!bytes)
        return ""
    var url = ""
    for (var b = 0; b < bytes.length; b++)
        url += String.fromCharCode(bytes[b])
    return url
}

function lnurlEndpointOk(url) {
    var lower = String(url || "").toLowerCase()
    if (lower.indexOf("https://") === 0)
        return true
    return lower.indexOf("http://") === 0 && /\.onion(\/|$)/.test(lower)
}

function fixedLnurlSats(body) {
    var raw
    try {
        raw = JSON.parse(body)
    } catch (e) {
        return 0
    }
    if (!raw || raw.tag !== "payRequest")
        return 0
    var min = Number(raw.minSendable)
    var max = Number(raw.maxSendable)
    if (!(min > 0) || min !== max || min % 1000 !== 0)
        return 0
    return min / 1000
}

function finishLnurl(link, sats) {
    if (sats > 0)
        lnurlFixedCache[link] = sats
    var waiters = lnurlWaiters[link] || []
    delete lnurlWaiters[link]
    for (var i = 0; i < waiters.length; i++)
        waiters[i](sats > 0 ? sats : 0)
}

// The amount is not in the QR. A fixed LNURL-pay amount is the pay
// request's minimum when it equals the maximum, in millisatoshis.
function resolveLnurl(text, callback) {
    var link = lnurlValue(text)
    if (!link) {
        callback(0)
        return
    }
    if (Object.prototype.hasOwnProperty.call(lnurlFixedCache, link)) {
        callback(lnurlFixedCache[link])
        return
    }
    if (lnurlWaiters[link]) {
        lnurlWaiters[link].push(callback)
        return
    }
    lnurlWaiters[link] = [callback]
    var endpoint = decodeLnurl(link)
    if (!lnurlEndpointOk(endpoint)) {
        finishLnurl(link, 0)
        return
    }
    var xhr = new XMLHttpRequest()
    xhr.onreadystatechange = function() {
        if (xhr.readyState !== XMLHttpRequest.DONE)
            return
        finishLnurl(link, fixedLnurlSats(xhr.responseText))
    }
    xhr.open("GET", endpoint)
    xhr.send()
}

function lnurlValue(text) {
    var s = String(text || "").replace(/^\s+|\s+$/g, "")
    if (s.toLowerCase().indexOf("lightning:") === 0)
        s = s.substring("lightning:".length).replace(/^\s+|\s+$/g, "").replace(/^\/+/, "")
    if (s !== s.toLowerCase() && s !== s.toUpperCase())
        return ""
    if (!/^lnurl1[02-9ac-hj-np-z]+$/i.test(s))
        return ""
    return s
}

function lightningAddress(text) {
    var s = String(text || "").replace(/^\s+|\s+$/g, "")
    if (s.toLowerCase().indexOf("lightning:") === 0)
        s = s.substring("lightning:".length).replace(/^\s+|\s+$/g, "")
    s = s.toLowerCase()
    // Same shape Noah accepts: an email-like name whose user part is
    // lowercase and whose domain is not a Tor onion address.
    if (!/^[a-z0-9._-]+@[a-z0-9.-]+\.[a-z]{2,4}$/.test(s))
        return ""
    var at = s.indexOf("@")
    var user = s.substring(0, at)
    var domain = s.substring(at + 1)
    if (!/^[a-z0-9_.-]+$/.test(user) || /\.onion$/.test(domain))
        return ""
    return s
}

function payUri(method, parsed, amountSat) {
    var amount = formatBTC(amountSat)
    if (method === "lightning" && parsed.lnAddress) {
        var addrUri = "bitcoin:?lnaddr=" + encodeURIComponent(parsed.lnAddress)
        if (amountSat > 0)
            addrUri += "&amount=" + amount
        return addrUri
    }
    if (method === "lightning" && parsed.lnurl) {
        var linkUri = "bitcoin:?lnurl=" + encodeURIComponent(parsed.lnurl)
        if (amountSat > 0)
            linkUri += "&amount=" + amount
        return linkUri
    }
    if (method === "lightning") {
        var uri = "bitcoin:?lightning=" + encodeURIComponent(parsed.lightning)
        if (bolt11Amount(parsed.lightning) < 0 && amountSat > 0)
            uri += "&amount=" + amount
        return uri
    }
    if (method === "ark")
        return "bitcoin:?ark=" + encodeURIComponent(parsed.ark) + "&amount=" + amount
    return "bitcoin:" + parsed.onchain + "?amount=" + amount
}
