# Handover APP Agent: Filtering Tujuan OUT SPP

Dokumen ini mandiri. APP Agent tidak memerlukan akses repository atau commit backend untuk mengimplementasikannya.

## 1. Tujuan Perubahan

Untuk movement **OUT dengan sumber SPP**, APP harus menampilkan tujuan berdasarkan field `allowedOutDestinations` dari UnitLog BE. Jangan menentukan kelayakan sendiri dari histori, status yang tersimpan di perangkat, nomor SPP, atau nama customer.

Perubahan ini tidak mengubah flow WO/OTHER, pilihan kategori IN, atau payload movement yang sudah berlaku. Jangan membuat endpoint baru atau memperluas permission pengguna.

## 2. Lookup SPP

QR SPP berisi nomor dokumen langsung, bukan URL. Isi nomor lengkap ke form setelah scan. Saat satpam menekan **Cek SPP**, panggil:

```http
GET <UNITLOG_BASE_URL>/api/v1/movement-documents/spp?number=<URL-encoded-SPP>
Authorization: Bearer <token-user>
```

- Permission tetap `unitlog:app:default:view`.
- Trim nomor sebelum mengirim dan lakukan URL encoding pada nomor lengkap. Jangan memotong nomor yang mengandung `/`.
- Android hanya memanggil UnitLog. Jangan memanggil SAOS langsung, memakai service token, atau memanggil endpoint unit/history yang khusus web.
- Lookup hanya mengisi form dan menghitung kelayakan. Lookup tidak membuat movement atau mereservasi dokumen.

## 3. Respons Sukses

```json
{
  "data": {
    "noLambung": "SAMT119",
    "noPolisi": "DA 1234 AB",
    "driverName": "Driver Name",
    "sppNumber": "SPP-001",
    "customerId": "C400",
    "customerName": "Customer Name",
    "allowedOutDestinations": ["CUSTOMER"]
  }
}
```

`customerId` dan `customerName` dapat tidak tersedia pada dokumen tertentu. Jangan memakai nama customer sebagai identifier atau menghitung eligibility di APP berdasarkan field tersebut.

Backend terbaru selalu menyertakan `allowedOutDestinations` sebagai array, termasuk array kosong. Nilai yang dikenal untuk sumber SPP hanya:

```json
["CUSTOMER"]
```

```json
["FILLING_SHED_KUIN"]
```

```json
[]
```

### Penjelasan aturan BE, bukan untuk dihitung ulang oleh APP

| Kondisi | Hasil |
| --- | --- |
| IN Gantungan, SPP aktif dan kode customer cocok, lifecycle masih valid | `["CUSTOMER"]` |
| IN Gantungan tetapi SPP/customer tidak cocok atau cycle tidak tepercaya | `[]` |
| IN Kosongan atau Service dengan SPP yang belum dipakai untuk Filling Shed dan belum selesai | `["FILLING_SHED_KUIN"]` |
| Belum ada movement dengan SPP yang masih layak | `["FILLING_SHED_KUIN"]` |
| Unit sedang OUT | `[]` |
| Tidak ada departure baru yang memenuhi aturan dokumen/lifecycle | `[]` |

BE menggunakan movement aktif dan departure yang terhubung melalui cycle IN Gantungan tersebut, bukan sembarang histori OUT. Perubahan nama customer dengan kode customer yang tetap sama tidak menggagalkan kecocokan. Data lama tanpa cycle tepercaya tidak ditebak atau diperbaiki otomatis oleh APP.

## 4. Perubahan DTO dan State APP

1. Tambahkan `allowedOutDestinations` pada DTO **respons lookup SPP**.
2. Pertahankan perbedaan antara field tidak tersedia dan array kosong. Contoh Kotlin: `List<String>?`, dengan `null` sebagai unknown dan list kosong sebagai hasil BE yang diketahui.
3. Jangan menambahkan field ini ke DTO request movement, payload pending submission, atau hash/idempotency buatan APP.
4. Pastikan state lookup terkait dengan dokumen dan unit yang sedang ditampilkan. Respons asynchronous dari lookup lama tidak boleh menimpa form terbaru.

## 5. Aturan UI untuk OUT SPP Baru

- Setelah lookup sukses, tampilkan **hanya tujuan yang dikembalikan BE**. Jangan menampilkan Bengkel Luar atau OTHER sebagai tujuan dari sumber SPP.
- Setelah lookup baru, hapus pilihan tujuan lama jika sudah tidak ada pada array terbaru.
- Saat nomor dokumen berubah, invalidasikan hasil lookup dan selection lama sampai pengecekan dokumen baru selesai.
- Jika array `[]`, kosongkan pilihan tujuan dan nonaktifkan konfirmasi **OUT baru**. Tampilkan pesan umum bahwa unit/SPP belum memenuhi syarat keberangkatan. Respons tidak menyediakan alasan penolakan rinci; jangan mengarang alasannya.
- Jika field hilang, null, atau malformed, kelayakan belum diketahui. Jangan default ke kedua tujuan; minta lookup ulang atau BE yang kompatibel untuk OUT baru.
- Jangan mengizinkan enum yang tidak dikenal sebagai tujuan submit.

## 6. Penting: IN Tidak Boleh Terblokir

Endpoint lookup dipakai bersama untuk IN dan OUT. Unit yang sedang OUT dapat menerima respons dokumen valid dengan bagian data:

```json
{
  "allowedOutDestinations": []
}
```

Ini **bukan kegagalan lookup** dan **bukan alasan memblokir returning IN**. Tetap isi data unit/driver/customer dan lanjutkan form IN. Jangan otomatis mengganti arah yang dipilih pengguna.

Returning IN tetap harus memakai SPP yang sama dengan OUT keberangkatannya. BE memvalidasi hal ini saat submit.

## 7. Submit Movement Tetap Menggunakan Kontrak Lama

```http
POST <UNITLOG_BASE_URL>/api/v1/movements
Authorization: Bearer <token-user>
Idempotency-Key: <UUID-per-movement>
Content-Type: application/json
```

Contoh OUT Customer, hanya bila ditawarkan BE:

```json
{
  "noLambung": "SAMT119",
  "direction": "OUT",
  "outDestination": "CUSTOMER",
  "sppNumber": "SPP-001"
}
```

Untuk Filling Shed, gunakan `outDestination: "FILLING_SHED_KUIN"`, hanya bila ditawarkan BE.

Contoh returning IN:

```json
{
  "noLambung": "SAMT119",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "sppNumber": "SPP-001"
}
```

Kategori IN tetap: `WAITING_FOR_ASSIGNMENT`, `WAITING_FOR_DELIVERY_TO_CUSTOMER`, atau `SERVICE_AND_REPAIR_MAINTENANCE`.

Koordinat tetap satu string opsional, misalnya `"coordinates": "-3.319437,114.590889"`, berurutan latitude,longitude. Jika GPS tidak tersedia, field boleh dihilangkan. Jangan mengirim `allowedOutDestinations`, driver/customer hasil dokumen, cycle, atau timestamp client sebagai field tambahan pada payload SPP.

## 8. Submit Bisa Ditolak Setelah Lookup Sukses

Lookup bukan reservasi. Satpam lain dapat mengubah status sebelum submit. BE selalu memvalidasi ulang secara atomik.

| Respons submit | Tindakan APP |
| --- | --- |
| `404 SPP_NOT_FOUND` | Tampilkan `SPP tidak ditemukan di SAOS.`; tawarkan koreksi/scan ulang untuk percobaan baru, tanpa mengubah payload pending. |
| `422 INVALID_TRANSITION` | Status tidak mengizinkan aksi; pertahankan form dan tawarkan lookup ulang untuk percobaan baru. |
| `422 DOCUMENT_VALIDATION_FAILED` | Validasi dokumen, kecocokan unit/polisi, SPP aktif, customer, atau returning document gagal; tampilkan pesan aman dari BE. |
| `409 SPP_USED` | SPP tidak dapat dipakai untuk departure tersebut; tawarkan koreksi atau dokumen lain. |
| `409 IDEMPOTENCY_CONFLICT` | Key dipakai dengan payload berbeda; jangan otomatis mengganti payload/key untuk meneruskan retry yang hasilnya belum pasti. |

Pada lookup, dokumen/unit/polisi yang sungguh invalid tetap menghasilkan error, bukan sekadar array kosong. `500 INTERNAL_ERROR` adalah gangguan integrasi/database, bukan hasil eligibility `[]`.

## 9. Retry Harus Dipisahkan dari Form Movement Baru

- Simpan payload lengkap dan satu idempotency key sebelum mengirim.
- Setelah timeout atau hasil yang belum pasti, kirim ulang **payload asli dan key yang sama**.
- Jangan membangun ulang payload pending dari respons lookup, menambah eligibility array, mengambil GPS ulang, atau mengubah koordinat pada retry.
- Jangan memblokir retry hanya karena lookup terbaru menghasilkan `[]`. Submit pertama mungkin sudah berhasil dan mengubah status unit menjadi OUT; BE tetap dapat mengembalikan replay transaksi yang sama.
- Pending lama tanpa koordinat harus tetap dikirim tanpa penambahan koordinat.

## 10. Rute Langsung Filling Shed ke Customer

UnitLog hanya mencatat lintasan keluar/masuk pool:

- `Pool → Filling Shed → Customer → Pool`: satu `OUT FILLING_SHED_KUIN`, lalu returning IN dengan SPP yang sama. Tidak ada IN fiktif atau OUT Customer tambahan di luar pool.
- `Pool → Filling Shed → Pool (Gantungan) → Customer → Pool`: OUT Filling Shed, IN Gantungan, OUT Customer jika diizinkan BE, kemudian returning IN.

## 11. Deployment dan Acceptance Checklist

Deploy BE yang mendukung array ini sebelum merilis filtering APP. Fitur eligibility tidak membutuhkan permission atau migration database baru.

- [ ] Gantungan yang eligible hanya menampilkan Customer.
- [ ] Kosongan/Service yang eligible hanya menampilkan Filling Shed.
- [ ] Array kosong menonaktifkan OUT baru, tetapi tidak memblokir IN atau pending retry.
- [ ] Field hilang/null tidak dianggap sebagai izin untuk kedua tujuan.
- [ ] Lookup dokumen baru membersihkan selection yang tidak valid; respons lama tidak menimpa form terbaru.
- [ ] Field response-only tidak masuk payload submit.
- [ ] Perubahan status setelah lookup ditangani melalui error submit.
- [ ] Retry mempertahankan payload/key/koordinat asli.
- [ ] Tidak ada token, raw response, nama driver/customer, atau koordinat yang dicatat ke log/analytics tanpa kebutuhan yang disetujui.
- [ ] Jangan membuat movement live untuk pengujian tanpa persetujuan eksplisit.
