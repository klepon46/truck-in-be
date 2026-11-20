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

## Bukan pelonggaran OUT/lifecycle

Menerima IN SPP-B setelah OUT SPP-A **tidak** berarti B otomatis menjadi SPP aktif, A dikoreksi menjadi B, atau SPP mana pun boleh untuk OUT berikutnya.

Aturan WO sekali pakai, SPP Filling Shed/reuse/completion, Customer, Gantungan dan kelanjutan Servis tidak diubah. SQL lifecycle yang ada tetap berlaku pada dokumen/cycle tersimpan; SPP berbeda pada IN/OUT tidak menjadi korelasi tepercaya untuk continuation, sehingga lookup OUT A maupun B bisa menghasilkan `[]`. Jangan mengarang aturan SPP-C untuk OUT berikutnya atau memperbaiki histori otomatis.

Tetap tampilkan hanya `allowedOutDestinations` dari lookup untuk **OUT baru**. Array kosong tidak memblokir returning IN, tetapi validasi dokumen/unit/jenis sumber tetap berlaku.

## Retry dan error

- Simpan body, idempotency key dan koordinat asli sebelum kirim. Timeout/hasil belum pasti: retry tanpa mengubah semuanya.
- Jangan mengganti nomor pada request pending agar mengikuti scan atau aturan baru. Body berubah dengan key yang sudah tersimpan tetap `409 IDEMPOTENCY_CONFLICT`.
- Replay tersimpan tetap mengembalikan snapshot asli, tanpa bergantung pada dokumen upstream/eligibility saat ini.
- `422 DOCUMENT_VALIDATION_FAILED` tetap berlaku untuk jenis sumber/unit berbeda atau dokumen invalid; `404 WORK_ORDER_NOT_FOUND` / `SPP_NOT_FOUND` untuk absence terkonfirmasi. Gangguan upstream/database bukan izin untuk melewati pemeriksaan.

## Rollout dan pengujian

Tidak ada field/permission/config/migration tambahan. Deploy BE ini sebelum APP mengandalkan penerimaan nomor berbeda; kebutuhan schema sebelumnya (sampai 000006) tetap berlaku. APP yang memiliki validasi lokal “nomor IN harus sama dengan OUT” perlu melepas pembatasan nomor saja, bukan jenis sumber/unit.

Rollback image lama mengembalikan aturan nomor sama untuk **submit baru**; replay yang sudah tersimpan tetap kompatibel. Jangan mengubah pending payload atau memutasi histori untuk memaksakan rollback.

Uji nonproduksi: WO-A→WO-B, SPP-A→SPP-B untuk semua kategori IN, jenis/unit salah ditolak tanpa write parsial, field dokumen IN/departure berbeda, cycle/durasi benar, hash/replay/koordinat tetap, dan OUT setelah SPP berbeda tetap mengikuti array BE. Tidak perlu movement live.

### Catatan backend

Submit mengunci unit terlebih dahulu. IN dengan SPP berbeda kemudian mengunci kedua canonical SPP menggunakan distinct numeric advisory keys berurutan sebelum INSERT/penutupan cycle; ini menjaga serialisasi completion dan menghindari urutan lock terbalik A→B/B→A. Tidak ada perubahan trigger/index/schema.

Referensi: [PostgreSQL lock ordering](https://www.postgresql.org/docs/15/explicit-locking.html#LOCKING-DEADLOCKS), [transaction advisory locks](https://www.postgresql.org/docs/15/explicit-locking.html#ADVISORY-LOCKS), dan [evaluasi output SELECT setelah sorting](https://www.postgresql.org/docs/15/sql-select.html#SQL-SELECT-LIST).
