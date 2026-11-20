# APP Handover — Returning IN Boleh Berbeda Nomor WO/SPP

## Tujuan dan batas perubahan

Satpam dapat mencatat unit kembali IN meskipun nomor WO/SPP pada OUT sebelumnya salah dipindai. **Jenis sumber dan unit tetap sama; nomor dokumen boleh berbeda.**

| OUT sebelumnya | Scan returning IN | Hasil bila validasi lain lolos |
| --- | --- | --- |
| WO-A, SAMT027 | WO-B, SAMT027 | Boleh |
| SPP-A, SAMT027 | SPP-B, SAMT027 | Boleh |
| WO-A | SPP-B atau OTHER | Ditolak |
| SPP-A | WO-B atau OTHER | Ditolak |
| OTHER | WO/SPP | Ditolak; tetap OTHER |
| Unit SAMT027 | Dokumen unit lain | Ditolak |

WO memakai upstream `vehicle_no`; SPP memakai `unit_name`. Dokumen harus ditemukan/valid dan cocok dengan master unit aktif. WO tetap memeriksa polisi; SPP tetap memakai polisi master dan mengabaikan polisi free text SAOS. Tidak ada picker fallback, flag bypass, atau akses upstream langsung dari APP.

## Endpoint dan payload tetap

Android tetap lookup melalui UnitLog, lalu submit:

```http
POST <UNITLOG_BASE_URL>/api/v1/movements
Authorization: Bearer <JWT-user>
Idempotency-Key: <UUID-per-movement>
Content-Type: application/json
```

Permission tetap `unitlog:app:default:view`.

Contoh OUT awal memakai WO-A, kemudian returning IN memakai WO-B:

```json
{
  "noLambung": "SAMT027",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "workOrderNumber": "WO-B"
}
```

Untuk SPP, gunakan **hanya** `sppNumber: "123-example"` sebagai pengganti `workOrderNumber`. SPP tetap referensi QR `{id}-{owner}`, bukan canonical `trans_no` dari histori. Kategori IN yang tersedia tidak berubah. `coordinates` tetap string opsional `latitude,longitude`.

Jangan mengganti hasil scan dengan nomor departure lama. Jangan kirim source transaction ID, cycle ID, driver/customer hasil dokumen, status lama, atau flag pengecualian.

## Histori dan cycle

- OUT tetap immutable, membawa WO/SPP yang benar-benar dipakai saat OUT.
- IN menyimpan WO yang dipindai atau canonical SPP hasil resolusi scan IN.
- Cycle tetap memasangkan OUT awal dan IN ini untuk unit yang sama; tidak dibuat OUT pengganti/fiktif.
- Respons IN `workOrderNumber`/`sppNumber` menunjukkan dokumen IN.
- `departureWorkOrderNumber`/`departureSppNumber` menunjukkan dokumen OUT asli dan **boleh berbeda** dari field IN.
- Tampilkan kedua dokumen sesuai konteks; jangan anggap perbedaan nomor sebagai payload invalid atau menimpa salah satunya. `cycleId` dan `departureTransactionId` tetap berasal dari BE.
- OUT duration difinalkan saat IN berhasil; kegagalan membatalkan seluruh movement/cycle/duration/status.

## OUT berikutnya mengikuti policy 7

Menerima IN SPP-B setelah OUT SPP-A **tidak** mengoreksi dokumen OUT A menjadi B. OUT berikutnya tetap memerlukan dokumen valid, unit sama, status IN, dan hasil eligibility terbaru.

Perubahan returning IN sendiri tidak menulis ulang dokumen/cycle lama. Perubahan berikutnya, policy 7, mengizinkan SPP valid pada unit sama dipakai berulang ke Filling Shed atau Customer setelah kategori IN apa pun, tanpa harus cocok dengan SPP/customer IN sebelumnya. Customer tetap memerlukan kode customer hasil scan; histori lintas unit tetap memblokir. WO sekali pakai dan batas WO/OTHER tetap berlaku. Lihat [panduan policy 7](mobile-spp-reuse-handover.md).

Tetap tampilkan hanya `allowedOutDestinations` dari lookup untuk **OUT baru**. Array kosong tidak memblokir returning IN, tetapi validasi dokumen/unit/jenis sumber tetap berlaku.

## Retry dan error

- Simpan body, idempotency key dan koordinat asli sebelum kirim. Timeout/hasil belum pasti: retry tanpa mengubah semuanya.
- Jangan mengganti nomor pada request pending agar mengikuti scan atau aturan baru. Body berubah dengan key yang sudah tersimpan tetap `409 IDEMPOTENCY_CONFLICT`.
- Replay tersimpan tetap mengembalikan snapshot asli, tanpa bergantung pada dokumen upstream/eligibility saat ini.
- `422 DOCUMENT_VALIDATION_FAILED` tetap berlaku untuk jenis sumber/unit berbeda atau dokumen invalid; `404 WORK_ORDER_NOT_FOUND` / `SPP_NOT_FOUND` untuk absence terkonfirmasi. Gangguan upstream/database bukan izin untuk melewati pemeriksaan.

## Rollout dan pengujian

Tidak ada field/permission/config tambahan. Penerimaan nomor IN berbeda sendiri tidak menambah migration, tetapi BE terbaru memerlukan schema sampai **000007** untuk policy SPP reuse. Deploy migration sebelum BE, lalu APP. APP perlu melepas validasi lokal “nomor IN harus sama dengan OUT”, bukan jenis sumber/unit.

Image sebelum fitur returning IN berbeda nomor akan kembali mensyaratkan nomor sama untuk **submit baru**; fitur ini tidak mengubah hash/replay tersimpan. Kompatibilitas rollback BE/schema setelah policy 7 tetap harus ditinjau sesuai [batas rollback terbaru](mobile-spp-reuse-handover.md). Jangan mengubah pending payload atau memutasi histori untuk memaksakan rollback.

Uji nonproduksi: WO-A→WO-B, SPP-A→SPP-B untuk semua kategori IN, jenis/unit salah ditolak tanpa write parsial, field dokumen IN/departure berbeda, cycle/durasi benar, hash/replay/koordinat tetap, dan OUT setelah SPP berbeda tetap mengikuti array BE. Tidak perlu movement live.

### Catatan backend

Submit mengunci unit terlebih dahulu. IN dengan SPP berbeda kemudian mengunci kedua canonical SPP menggunakan distinct numeric advisory keys berurutan sebelum INSERT/penutupan cycle; ini menjaga serialisasi histori dan menghindari urutan lock terbalik A→B/B→A. Policy 7 terpisah mengubah trigger/index/schema melalui migration 000007.

Referensi: [PostgreSQL lock ordering](https://www.postgresql.org/docs/15/explicit-locking.html#LOCKING-DEADLOCKS), [transaction advisory locks](https://www.postgresql.org/docs/15/explicit-locking.html#ADVISORY-LOCKS), dan [evaluasi output SELECT setelah sorting](https://www.postgresql.org/docs/15/sql-select.html#SQL-SELECT-LIST).
