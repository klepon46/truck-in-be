# Handover: Validasi Driver OTHER IN/OUT Sementara

## Tujuan dan Batasan

Pencarian driver dapat berhasil, tetapi validasi ulang pada submit mendapat `404` dari Driver service. Saat flag validasi aktif, BE mencari nama lengkap dari payload submit dan mencocokkan ID/nama pada hasil driver aktif. Hasil picker sebelumnya bukan reservasi atau bukti bahwa driver masih aktif saat submit.

Untuk uji coba yang disetujui, tersedia bypass **hanya panggilan Driver saat submit OTHER IN dan OUT**, bukan penghapusan semua validasi. Default tetap aman: validasi aktif. Ini tidak memperbaiki perilaku pencarian nama upstream; perbaikan permanen seperti lookup berdasarkan ID membutuhkan kontrak Driver yang terkonfirmasi.

## Aktivasi oleh Operator

1. Deploy BE yang mendukung flag ini.
2. Pada JSON konfigurasi Consul environment yang dituju (`dev/be/truckin-be-config` untuk development), tambahkan atau ubah hanya field berikut; pertahankan field konfigurasi lainnya:

   ```json
   { "validate_other_driver_on_submit": false }
   ```

   Ini potongan konfigurasi, bukan pengganti seluruh value Consul. Jangan menaruh token atau DSN di dokumen/chat.
3. Restart seluruh instance BE environment tersebut. Konfigurasi dibaca saat startup, bukan setiap request. Jangan membiarkan instance campuran true/false selama uji coba.
4. Pastikan startup menampilkan warning `OTHER driver submit validation disabled`. Warning tidak memuat nama driver, token, atau koordinat.
5. Setelah uji coba, kembalikan nilai menjadi `true` (atau hapus field), lalu restart seluruh instance lagi.

Field tidak ada atau `null` berarti `true`. Nilai harus boolean JSON; string `"false"`, angka `0`, array, atau object ditolak saat startup. Tidak ada migration baru. Penerapan kode ini tidak mengubah konfigurasi Consul live secara otomatis.

**Risiko:** saat false, BE tidak menjamin driver masih aktif atau ID/nama dari APP tidak dimanipulasi. Snapshot driver yang disimpan bersifat APP-reported, bukan terverifikasi saat submit, dan tetap immutable. Mengaktifkan kembali validasi tidak memverifikasi ulang atau mengubah audit record lama; respons tidak menambahkan field penanda verifikasi.

## Kontrak APP Tidak Berubah

- Android hanya memanggil UnitLog, tidak Driver service langsung.
- Picker tetap `GET /api/v1/drivers?name=<URL-encoded-query>` dan tetap mengambil driver aktif. URL/token Driver tetap wajib di konfigurasi BE.
- Satpam tetap memilih driver dari hasil picker. Kirim `driverId` dan `driverName` pasangan hasil tersebut, jangan ketik nama bebas atau mengganti ID/nama.
- Submit tetap `POST /api/v1/movements` dengan token pengguna, permission `unitlog:app:default:view`, actor claims, dan `Idempotency-Key` UUID per movement.
- OTHER IN/OUT tetap membutuhkan `driverId > 0`, `driverName` tidak kosong setelah trim, dan `note` tidak kosong setelah trim. Jangan kirim WO/SPP pada sumber OTHER.
- Jangan menambahkan `validate_other_driver_on_submit` ke payload/DTO/request hash APP; flag hanya dapat ditentukan operator di BE.
- Koordinat opsional tetap satu string `latitude,longitude`. Simpan payload dan key sebelum submit; pending retry mempertahankan ID/nama/note/koordinat/key asli walau flag BE berubah.

Contoh OUT OTHER:

```json
{
  "noLambung": "SAMT025",
  "direction": "OUT",
  "outDestination": "OTHER",
  "driverId": 42,
  "driverName": "Driver Name",
  "note": "Operational note"
}
```

Contoh IN OTHER:

```json
{
  "noLambung": "SAMT025",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "driverId": 42,
  "driverName": "Driver Name",
  "note": "Operational note"
}
```

Kategori IN lainnya tetap sesuai kontrak yang sudah berlaku. Contoh ini bukan izin membuat movement live.

## Validasi yang Tetap Berlaku

- Unit harus lolos validasi snapshot aktif dan transisi harus sah; IN → IN dan OUT → OUT tetap ditolak.
- OTHER OUT tidak dapat melewati aturan Gantungan yang hanya mengizinkan OUT Customer dengan SPP aktif.
- Returning IN dari keberangkatan WO/SPP tetap membutuhkan dokumen yang sama; OTHER tidak dapat dipakai sebagai bypass.
- WO/SPP tetap divalidasi melalui integrasinya. Flag ini tidak memengaruhi lookup, eligibility SPP, atau customer.
- Idempotency, atomic commit/rollback, cycle, durasi, koordinat, permission, dan snapshot audit tetap berjalan.
- `422 VALIDATION_ERROR`, `422 INVALID_TRANSITION`, `422 DOCUMENT_VALIDATION_FAILED`, `404 UNIT_NOT_FOUND`, dan `409 IDEMPOTENCY_CONFLICT` tetap harus ditangani sesuai kontrak. Bypass bukan jaminan submit selalu berhasil.

## Verifikasi

- Default/true: pencarian picker berhasil tetapi lookup ulang nama lengkap yang mendapat upstream 404 masih ditolak.
- False: OTHER IN dan OUT tidak memanggil Driver saat submit; picker tetap memanggilnya.
- ID negatif/nol, nama/note kosong, unit tidak dikenal, dan transisi/dokumen return salah tetap ditolak.
- Snapshot nama ter-trim, koordinat, cycle, dan durasi tetap diproses; replay transaksi tersimpan tetap berhasil walau flag berubah.
- Jangan membuat movement live untuk pengujian tanpa persetujuan eksplisit.
