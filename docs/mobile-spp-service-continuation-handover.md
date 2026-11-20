# APP Handover — SPP Dilanjutkan Setelah IN Servis

**Policy terbaru (migration 000007):** kedua tujuan SPP sekarang terbuka setelah semua kategori IN; SPP boleh digunakan berulang pada unit sama, termasuk setelah Kosongan. Panduan ini mempertahankan contoh Servis, tetapi pengecualian “hanya Servis” dan aturan Kosongan terminal versi 000006 sudah digantikan. Lihat [panduan policy 7](mobile-spp-reuse-handover.md).

Dokumen mandiri untuk Android/satpam. Android hanya memanggil UnitLog, bukan SAOS atau endpoint histori/web. Permission tetap `unitlog:app:default:view`.

## Flow yang didukung

```text
OUT Filling Shed — SPP-A
→ IN Servis — SPP-A
→ OUT Filling Shed lagi — SPP-A
→ IN Gantungan — SPP-A
→ OUT Customer — SPP-A
→ IN Kosongan — cycle ditutup; SPP-A boleh dipakai lagi pada unit sama
```

Servis dapat berulang. Kategori tetap `SERVICE_AND_REPAIR_MAINTENANCE`; jangan mengubahnya menjadi Gantungan. Setiap lintasan OUT/IN tetap transaksi/cycle terpisah.

## Lookup dan pilihan tujuan

```http
GET <UNITLOG_BASE_URL>/api/v1/movement-documents/spp?number=123-example
Authorization: Bearer <JWT-user>
```

`number` adalah referensi QR `{id}-{owner}`, di-URL-encode. BE mencocokkan canonical SPP hasil SAOS, bukan string referensi QR.

| Kondisi | `data.allowedOutDestinations` |
| --- | --- |
| IN Kosongan/Gantungan/Servis; SPP valid pada unit sama, customer hasil scan tersedia; baru maupun reuse | `["CUSTOMER","FILLING_SHED_KUIN"]` |
| Kategori IN tersebut dengan kode customer hasil scan kosong, atau belum ada current movement | `["FILLING_SHED_KUIN"]` |
| Current OUT atau canonical SPP memiliki histori pada unit lain | `[]` |

Tampilkan hanya tujuan yang dikembalikan BE. Bersihkan pilihan lama saat lookup berubah; abaikan respons lookup yang sudah stale. Field hilang/null/malformed berarti unknown, bukan izin kedua tujuan. Jangan hitung ulang aturan dari status lokal.

Array kosong hanya membatasi **OUT baru**. Tetap isi form returning IN; jangan blokir IN atau pending retry karena array kosong. Lookup bukan reservasi.

## Submit tidak menambah field

```http
POST <UNITLOG_BASE_URL>/api/v1/movements
Authorization: Bearer <JWT-user>
Idempotency-Key: <UUID-per-movement>
Content-Type: application/json
```

```json
{
  "noLambung": "SAMT119",
  "direction": "OUT",
  "outDestination": "FILLING_SHED_KUIN",
  "sppNumber": "123-example",
  "coordinates": "-3.319437,114.590889"
}
```

Pilih `CUSTOMER` bila ditawarkan dan sesuai perjalanan. Returning Servis memakai `direction: "IN"`, `inCategory: "SERVICE_AND_REPAIR_MAINTENANCE"`, tanpa `outDestination`. Returning IN boleh memakai SPP berbeda selama jenis sumber dan unit sama. OUT selanjutnya boleh memakai SPP valid pada unit sama, baru maupun reuse, tanpa kewajiban cocok dengan SPP/customer IN sebelumnya. Tetap gunakan hasil lookup terkini. Lihat [aturan returning IN](mobile-returning-document-handover.md).

Jangan kirim flag continuation, source transaction ID, `allowedOutDestinations`, customer/driver hasil dokumen, atau cycle ID. Koordinat tetap opsional; polisi berasal dari master Unit Lambung. Payload WO/OTHER tidak berubah.

Simpan payload/key sebelum kirim. Timeout: retry **body/key/koordinat asli**, tanpa lookup ulang sebagai syarat retry. Replay tetap mengembalikan snapshot asli meskipun unit kini OUT atau SPP sudah selesai. Dua alias QR canonical sama tidak mengizinkan reuse ilegal; mengganti referensi dengan key lama tetap konflik.

Untuk percobaan baru, tangani `422 INVALID_TRANSITION`, `422 DOCUMENT_VALIDATION_FAILED`, atau `409 SPP_USED` dengan mempertahankan form dan menawarkan lookup ulang. `404 SPP_NOT_FOUND` berbeda dari dokumen valid dengan `[]`; error integrasi/database tetap `500`, bukan eligibility kosong.

## Prasyarat dan verifikasi

Operator menerapkan migration sampai **000007**, lalu BE ini, sebelum mengandalkan flow baru. Tidak ada permission/konfigurasi/upstream baru. APP yang sudah mendukung daftar tujuan dinamis cukup menampilkan dua pilihan, tanpa request baru.

Uji pada environment nonproduksi: flow contoh, pengulangan Filling Shed/Customer setelah semua kategori IN termasuk Kosongan, SPP berbeda pada IN/OUT untuk unit sama, unit lain ditolak, Customer tanpa kode customer ditolak, returning IN dengan `[]`, serta retry setelah timeout. Jangan membuat movement live tanpa persetujuan.
