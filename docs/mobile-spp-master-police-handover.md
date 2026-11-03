# APP Handover: Nomor Polisi SPP Dari Master Unit Lambung

## Perubahan BE

Khusus SPP, `unit_police_no` dari SAOS adalah free text tanpa format yang dapat dipercaya. BE mengabaikannya, bukan mencoba regex, substring, atau menebak plat. Nilai berbeda, tambahan keterangan seperti `DA 8957 JI (SAFT 08)`, hilang, null, kosong, atau whitespace tidak lagi membuat SPP ditolak. Row SPP yang hanya berbeda pada field polisi boleh berulang; perbedaan nomor resmi/unit/driver/customer tetap ditolak.

- Lookup `data.noPolisi` berasal dari **master Unit Lambung** yang di-resolve melalui `unit_name` SAOS.
- Submit SPP IN/OUT baru menyimpan `noPolisi` dari **master unit yang dikunci saat submit**, bukan SAOS/APP. Master dapat berubah setelah lookup; transaksi memakai snapshot master saat submit.
- `unit_name` tetap wajib, harus cocok dengan `noLambung` request, dan harus resolve ke unit aktif. Tidak ada fallback picker atau inferensi berdasarkan polisi.
- WO tetap mewajibkan dan memvalidasi kecocokan nomor polisi Workshop. OTHER tidak berubah.
- Histori lama tidak diperbarui; replay mengembalikan snapshot asli, bukan polisi master terbaru.

Konsekuensi yang disetujui: bila SAOS memberikan No Lambung yang salah tetapi valid/aktif, pemeriksaan silang polisi SAOS tidak lagi dapat mendeteksinya. Jangan menambah pembanding polisi SAOS di APP.

## Kontrak APP Tetap

APP hanya memanggil UnitLog; jangan memanggil SAOS, Unit Lambung, Workshop, atau Driver langsung.

```http
GET /api/v1/movement-documents/spp?number=123-example
```

Tampilkan `noLambung`, `noPolisi`, driver, dan customer dari respons BE sebagai read-only. Pertahankan referensi utuh `{id}-{owner}` untuk request baru, misalnya:

```json
{
  "noLambung": "SAMT072",
  "direction": "OUT",
  "outDestination": "FILLING_SHED_KUIN",
  "sppNumber": "123-example"
}
```

Jangan menambahkan `noPolisi`/override polisi pada request. Lookup mengembalikan referensi input dalam `sppNumber`, sedangkan respons movement/history memakai canonical SAOS `trans_no`. `allowedOutDestinations` selalu array, termasuk `[]`; array kosong tidak memblokir lookup returning IN. Aturan eligibility, matching returning document/customer, permission, koordinat, siklus, durasi, dan idempotensi tidak berubah.

Retry pending harus mempertahankan body, `Idempotency-Key`, dan koordinat aslinya. Jangan menyusun ulang body dari lookup baru atau nomor canonical pada respons transaksi. Tidak diperlukan migrasi database, konfigurasi baru, ataupun perubahan payload APP; deploy BE agar kebijakan baru berlaku. Rollback BE mengaktifkan lagi pemeriksaan polisi SAOS pada lookup/submit baru, tetapi tidak mengubah snapshot tersimpan.

## Error Yang Tetap Berlaku

- `422 DOCUMENT_VALIDATION_FAILED`: dokumen invalid, unit salah, atau customer/return/lifecycle tidak sesuai. Bukan lagi karena polisi free text SAOS.
- Jika `unit_name` hilang/null/kosong/whitespace: `422 DOCUMENT_VALIDATION_FAILED`, pesan **Kode Unit Lambung tidak tersedia pada data SPP dari SAOS. Hubungi operasional untuk memeriksa data unit.**
- `404 UNIT_NOT_FOUND`: unit tidak resolve ke master aktif; berbeda dari `404 SPP_NOT_FOUND` / **SPP tidak ditemukan di SAOS.**
- Malformed referensi: lookup `400 INVALID_DOCUMENT_QUERY`, fresh submit `422 VALIDATION_ERROR`. Gangguan integrasi/database tetap `500 INTERNAL_ERROR`.

Gunakan status/`error.code` untuk alur dan tampilkan pesan aman BE. Jangan parsing pesan atau melewati validasi unit. Lihat [handover referensi](mobile-spp-id-owner-handover.md) dan [eligibility](mobile-spp-out-eligibility-handover.md) untuk kontrak lengkap.
