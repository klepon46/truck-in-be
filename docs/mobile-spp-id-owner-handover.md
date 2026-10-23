# Handover APP: Referensi SPP `{id}-{owner}`

Dokumen mandiri untuk APP Agent. Tidak memerlukan akses commit/repository BE atau SAOS.

## 1. Kontrak Input Baru

QR/form SPP sekarang membawa string gabungan, contoh **`123-example`**:

- `id` = `123`
- `owner` = `example`

Android tetap hanya memanggil UnitLog, dengan Bearer token pengguna dan permission `unitlog:app:default:view`. Jangan memanggil SAOS langsung atau menyimpan service token.

Saat **Cek SPP**:

```http
GET <UNITLOG_BASE_URL>/api/v1/movement-documents/spp?number=123-example
Authorization: Bearer <token-user>
```

Gunakan URL builder untuk meng-encode nilai `number` lengkap satu kali. Owner dapat mengandung spasi, tanda baca, dan hyphen; jangan uppercase/lowercase, memotong, atau memecahnya di APP. Backend memisahkan pada **hyphen pertama**, memangkas spasi di kedua komponen, lalu mengirim parameter `id` dan `owner` terpisah ke SAOS. `123-Example-East` berarti ID `123`, owner `Example-East`.

## 2. Lookup dan Audit Memiliki Arti `sppNumber` Berbeda

Contoh respons lookup:

```json
{
  "data": {
    "noLambung": "SAMT072",
    "noPolisi": "DA 8729 CB",
    "driverName": "Driver Name",
    "sppNumber": "123-example",
    "customerId": "C400",
    "customerName": "Customer Name",
    "allowedOutDestinations": ["FILLING_SHED_KUIN"]
  }
}
```

- **Lookup `data.sppNumber`** mengembalikan referensi input yang di-trim, untuk dipertahankan dalam form dan dikirim saat submit.
- **Request movement `sppNumber`** juga referensi `{id}-{owner}`, bukan `trans_no` asli.
- **Respons movement/replay/history `sppNumber`** adalah nomor resmi `trans_no` hasil SAOS, misalnya `SPG-BJM-2612481`, karena nilai inilah yang disimpan dalam audit dan dipakai lifecycle/cycle. `departureSppNumber` juga nomor resmi.
- Jangan menimpa referensi form/pending dengan `sppNumber` dari respons movement/history. Jangan mencoba menebak ID/owner dari nomor resmi. Tidak ada field baru yang perlu ditambahkan ke request.

Backend memvalidasi hasil ID/owner sebagai satu dokumen konsisten. Duplikasi baris dengan snapshot dokumen identik diterima; hasil yang bertentangan ditolak, bukan dipilih baris pertama. Ini bukan logic yang perlu ditiru APP.

## 3. Submit IN dan OUT

```http
POST <UNITLOG_BASE_URL>/api/v1/movements
Authorization: Bearer <token-user>
Idempotency-Key: <UUID-per-movement>
Content-Type: application/json
```

Contoh OUT Filling Shed, hanya jika ditawarkan BE:

```json
{
  "noLambung": "SAMT072",
  "direction": "OUT",
  "outDestination": "FILLING_SHED_KUIN",
  "sppNumber": "123-example"
}
```

Contoh returning IN Gantungan:

```json
{
  "noLambung": "SAMT072",
  "direction": "IN",
  "inCategory": "WAITING_FOR_DELIVERY_TO_CUSTOMER",
  "sppNumber": "123-example"
}
```

Kategori IN lain dan tujuan Customer tetap mengikuti aturan lama. Returning IN harus resolve ke nomor resmi SPP yang sama dengan departure. OUT lama yang tersimpan memakai `trans_no` tetap bisa ditutup memakai referensi baru yang resolve ke SPP itu; tidak ada perubahan audit/backfill.

Pilihan OUT tetap hanya dari `allowedOutDestinations`. `[]` bukan kegagalan lookup dan tidak memblokir returning IN. Jangan kirim array ini, customer, atau driver hasil dokumen pada request SPP. Koordinat tetap opsional satu string `latitude,longitude`.

## 4. Error

| Kondisi | Respons UnitLog |
| --- | --- |
| Referensi tanpa delimiter, ID kosong, atau owner kosong pada lookup | `400 INVALID_DOCUMENT_QUERY` |
| Format referensi salah pada submit baru | `422 VALIDATION_ERROR` |
| Hasil sukses SAOS kosong (`[]`/`null`) | `404 SPP_NOT_FOUND`, pesan `SPP tidak ditemukan di SAOS.` |
| `unit_name` SAOS hilang, null, kosong, atau hanya spasi | `422 DOCUMENT_VALIDATION_FAILED`, pesan `Kode Unit Lambung tidak tersedia pada data SPP dari SAOS. Hubungi operasional untuk memeriksa data unit.` |
| Dokumen tidak lengkap atau hasil ID/owner bertentangan | `422 DOCUMENT_VALIDATION_FAILED` |
| Unit/polisi/return/customer/lifecycle tidak cocok | Error validasi yang sudah berlaku; jangan bypass |
| Autentikasi/service upstream, network, atau envelope malformed | `500 INTERNAL_ERROR`, bukan dokumen tidak ditemukan |

Tetap cabangkan perilaku menggunakan `error.code`. `404 UNIT_NOT_FOUND` adalah masalah unit, bukan dokumen. Saat nomor diubah/lookup gagal, bersihkan hasil lookup/selection lama untuk form baru dan abaikan respons asynchronous usang.

Untuk `DOCUMENT_VALIDATION_FAILED`, tampilkan pesan aman BE, jangan selalu menimpanya dengan pesan generik atau mencocokkan teks untuk percabangan. Pesan kode Unit Lambung tidak tersedia berlaku pada lookup dan submit SPP IN/OUT, bukan berarti unit pasti tidak ada di master Unit Lambung. Validasi tetap menolak; jangan memilih unit manual atau memakai nomor polisi sebagai fallback. Payload dan kode error tidak berubah.

## 5. Retry, Migrasi, dan Deployment

- Simpan payload lengkap, key, dan koordinat sebelum pengiriman pertama. Retry hasil belum pasti wajib memakai payload/key asli. Jangan mengambil GPS ulang atau mengganti referensi dengan nomor resmi dari respons.
- Hash idempotency dihitung dari request asli sebelum resolusi SAOS. Mengganti referensi dengan key sama tetap konflik meskipun dua referensi resolve ke SPP resmi yang sama.
- Transaksi tersimpan dari APP lama tetap bisa di-replay dengan payload/key lama tanpa upstream lookup. **Jangan mengonversi pending lama ke format baru.**
- Request lama yang belum pernah tersimpan tidak mendapat fallback ke endpoint `search` yang sudah diganti. Jika hasil pengiriman lama belum pasti, selesaikan retry/reconciliation terlebih dahulu; jangan membuat key/payload baru secara otomatis. Setelah status dipastikan, operator dapat memulai percobaan baru dengan referensi dari QR/sumber yang sah.
- Koordinasikan cutover SAOS yang menerima `id`/`owner`, deploy BE ini, lalu rilis/aktifkan flow APP yang mengirim format baru. Jangan mengasumsikan mixed versions tetap kompatibel. Tidak ada migration atau flag konfigurasi baru; konfigurasi base URL/token tetap berlaku.
- WO dan OTHER tidak berubah. Tidak ada token, nama, referensi ID/owner, raw body upstream, atau koordinat yang perlu dicatat ke log/analytics.
- Jangan membuat movement live untuk pengujian tanpa persetujuan eksplisit.

## 6. Acceptance Checklist

- [ ] Scan `123-example` dipakai utuh pada lookup dan submit IN/OUT.
- [ ] Owner dengan kapital, spasi, `+`, `&`, dan hyphen tidak diubah atau terpotong; query di-encode dengan URL builder.
- [ ] Lookup reference tidak tertukar dengan canonical audit/history number.
- [ ] Hasil lookup lama tidak dapat mengizinkan submit dokumen yang sudah berubah.
- [ ] Error format, tidak ditemukan, invalid, dan integrasi ditangani berbeda.
- [ ] Returning IN tetap cocok dengan dokumen departure; OUT eligibility tetap dari BE.
- [ ] Pending lama/baru mempertahankan payload/key/koordinat, termasuk setelah respons movement memuat nomor resmi berbeda.
