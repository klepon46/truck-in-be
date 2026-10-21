# Handover APP: WO/SPP Tidak Ditemukan

Dokumen ini mandiri; APP Agent tidak memerlukan akses commit atau repository BE/upstream.

## Perubahan Kontrak

UnitLog membedakan dokumen yang **tidak ditemukan** dari dokumen invalid dan gangguan integrasi. Android hanya memanggil UnitLog menggunakan token pengguna dengan permission `unitlog:app:default:view`; jangan memanggil Workshop/SAOS langsung atau menggunakan service token.

Berlaku pada:

```http
GET /api/v1/movement-documents/work-orders?number=<URL-encoded-full-WO>
GET /api/v1/movement-documents/spp?number=<URL-encoded-full-SPP>
POST /api/v1/movements
```

Pada POST, kontrak ini berlaku untuk validasi dokumen movement **baru IN maupun OUT**. Replay transaksi tersimpan tetap diproses sebelum pemeriksaan dokumen terkini.

### WO Tidak Ditemukan

HTTP `404`:

```json
{
  "error": {
    "code": "WORK_ORDER_NOT_FOUND",
    "message": "WO tidak ditemukan di Workshop."
  }
}
```

### SPP Tidak Ditemukan

HTTP `404`:

```json
{
  "error": {
    "code": "SPP_NOT_FOUND",
    "message": "SPP tidak ditemukan di SAOS."
  }
}
```

## Penanganan Android

1. Cabangkan perilaku menggunakan `error.code`, bukan mencocokkan teks `message` atau menyamakan semua HTTP 404.
2. Pada dua kode di atas, tampilkan pesan aman dari BE dan pertahankan nomor dokumen agar satpam dapat memperbaiki atau scan ulang.
3. Untuk form movement baru, invalidasikan hasil lookup lama yang tidak lagi berlaku: unit/driver/customer/eligibility dari dokumen sebelumnya tidak boleh membuat konfirmasi tetap aktif. Setelah nomor diperbaiki, lakukan **Cek WO/Cek SPP** ulang. Abaikan respons asynchronous untuk dokumen/form yang sudah berubah.
4. Jangan mengganti sumber secara otomatis ke OTHER, membuat movement, menebak data unit, atau melewati validasi dokumen.
5. Jika error berasal dari POST, pertahankan form untuk tindak lanjut. Jangan otomatis mengulang request dengan nomor atau idempotency key baru.

### Bedakan Error Lain

| HTTP / kode | Makna dan tindakan |
| --- | --- |
| `404 UNIT_NOT_FOUND` | Dokumen ditemukan tetapi unit tidak tersedia di snapshot aktif, atau unit picker tidak tersedia saat submit. Hubungi operasional/master data; bukan WO/SPP tidak ditemukan. |
| `422 DOCUMENT_VALIDATION_FAILED` | Data dokumen tidak valid/tidak lengkap, unit/polisi/customer tidak cocok, atau dokumen IN tidak sesuai keberangkatan. Tampilkan pesan BE, jangan bypass. |
| `422 INVALID_TRANSITION` | Status tidak lagi mengizinkan aksi; tawarkan pengecekan ulang untuk percobaan baru. |
| `500 INTERNAL_ERROR` | Gangguan integrasi/database, timeout, autentikasi upstream, atau respons malformed. Jangan menyuruh satpam mengganti nomor seolah dokumen pasti tidak ada. Ini berbeda dari `401 UNAUTHORIZED` pengguna APP. |
| `200` dengan `data.allowedOutDestinations: []` pada lookup SPP | Dokumen valid, tetapi tidak ada OUT baru yang eligible. Bukan dokumen tidak ditemukan; IN dan pending retry tidak diblokir oleh array ini. |

## Arti Konfirmasi Upstream

- Workshop mengonfirmasi tidak ditemukan melalui HTTP 404 dengan envelope `success:false`, `code:404`, `message:"Work order not found"`, tanpa data dokumen. Halaman 404 generik dari proxy/route bukan bukti WO tidak ada.
- SAOS V2 menggunakan hasil pencarian sukses. Jika tidak ada `trans_no` yang persis sama, UnitLog mengembalikan `SPP_NOT_FOUND`. Daftar kosong dapat berupa `[]` atau `null` karena producer mengembalikan nil slice saat tidak ada row; field `data` yang hilang atau bertipe salah tetap error integrasi.
- `SPP_NOT_FOUND` berarti SPP tidak tersedia pada lookup tersebut; bukan bukti SPP dihapus dari seluruh database. APP tidak perlu meniru parsing upstream atau menghitung alasan sendiri.

## Pending Retry dan Deployment

- Simpan payload lengkap dan satu `Idempotency-Key` sebelum pengiriman pertama. Setelah timeout/hasil tidak pasti, retry harus memakai payload/key asli, termasuk koordinat, bukan hasil lookup terbaru.
- Lookup baru yang gagal tidak boleh mengubah atau menghapus pending request yang hasilnya belum diketahui. Pisahkan state form baru dari pending retry.
- BE tetap mengembalikan transaksi tersimpan untuk replay yang identik walaupun dokumen upstream kemudian tidak tersedia; payload berbeda dengan key sama tetap `409 IDEMPOTENCY_CONFLICT`.
- Payload request, permission, koordinat, idempotency hash, cycle/durasi, dan flag OTHER tidak berubah. Tidak ada endpoint, konfigurasi, atau migration baru.
- Deploy BE terlebih dahulu, lalu APP dengan penanganan kode baru. Tetap sediakan fallback error umum untuk BE lama atau kode yang belum dikenal; jangan mengubah 422/500 lama menjadi klaim dokumen tidak ditemukan.

## Acceptance Checklist

- [ ] Kedua kode 404 ditangani pada Cek WO/SPP dan submit IN/OUT.
- [ ] Nomor input tetap terlihat, tetapi hasil lookup/selection lama tidak mengizinkan movement baru.
- [ ] `UNIT_NOT_FOUND`, invalid-document, dan gangguan integrasi tetap memiliki penanganan berbeda.
- [ ] Lookup SPP valid dengan eligibility kosong tidak dianggap gagal.
- [ ] Tidak ada fallback otomatis ke OTHER atau panggilan upstream langsung.
- [ ] Pending payload/key/koordinat tidak berubah karena lookup gagal.
- [ ] Jangan log token, raw response upstream, nama driver/customer, atau koordinat; jangan membuat movement live tanpa persetujuan eksplisit.
