# Handover APP/Operasional — SPP Reuse (Policy 7)

## Aturan terbaru

Migration **000007** menggantikan aturan SPP “Gantungan hanya Customer”, “Kosongan terminal”, dan “reuse hanya melalui kelanjutan Servis”.

| Status unit | Tujuan SPP OUT |
| --- | --- |
| IN Kosongan / `WAITING_FOR_ASSIGNMENT` | Customer dan Filling Shed |
| IN Gantungan / `WAITING_FOR_DELIVERY_TO_CUSTOMER` | Customer dan Filling Shed |
| IN Servis / `SERVICE_AND_REPAIR_MAINTENANCE` | Customer dan Filling Shed |
| Belum ada current movement | Filling Shed saja |
| Masih OUT | Tidak boleh OUT baru; returning IN tetap dapat lookup |

Syarat: dokumen upstream valid dan cocok dengan master unit aktif. **SPP yang sama boleh dipakai berulang pada unit yang sama**, termasuk setelah Kosongan dan OUT Filling Shed/Customer sebelumnya. SPP/customer pada IN sebelumnya tidak harus sama dengan OUT berikutnya; tidak wajib memiliki cycle kelanjutan Servis. Customer tetap memerlukan kode customer hasil scan yang tidak kosong. Histori canonical SPP pada unit lain tetap memblokir OUT.

WO tetap sekali pakai; aturan WO/Bengkel/OTHER tidak dilonggarkan. IN→IN dan OUT→OUT tetap ditolak. Returning IN harus memakai jenis sumber OUT sebelumnya (WO→WO, SPP→SPP, OTHER→OTHER) dan unit sama, tetapi nomor WO/SPP boleh berbeda.

## Kontrak APP tetap

- Lookup: `GET /api/v1/movement-documents/spp?number=<URL-encoded-id-owner>`.
- Submit: `POST /api/v1/movements`, permission `unitlog:app:default:view`, header `Idempotency-Key` UUID.
- `sppNumber` request tetap referensi QR `{id}-{owner}`, bukan canonical `trans_no` dari histori. BE menyimpan canonical dari dokumen yang benar-benar dipindai.
- Pilihan OUT hanya dari `allowedOutDestinations`: `["CUSTOMER","FILLING_SHED_KUIN"]`, `["FILLING_SHED_KUIN"]`, atau `[]`. Customer tanpa kode customer tidak ditawarkan. Missing/null/malformed bukan izin default.
- `[]` tidak berarti lookup dokumen gagal dan tidak memblokir returning IN. Lookup bukan reservasi; submit memeriksa ulang status/dokumen di bawah lock.
- Tidak ada field, flag bypass, permission, konfigurasi, atau akses upstream baru. Jangan kirim customer/driver hasil dokumen, cycle/source ID, policy version, atau array eligibility.
- Pending request harus di-retry dengan body/key/koordinat asli meskipun lookup terbaru berubah. Jangan mengganti referensi pending dengan nomor canonical.

Contoh urutan yang diperbolehkan: OUT Filling Shed SPP-A → IN Kosongan SPP-B → OUT Customer SPP-A → IN Servis SPP-A → OUT Filling Shed SPP-A. Semua dokumen tetap harus valid untuk unit sama. Movement hanya dicatat saat melintasi batas pool; jangan membuat OUT Customer fiktif ketika unit masih berada di luar pool.

## Histori dan audit

Setiap OUT membuat cycle baru; returning IN menutup cycle OUT asli. Dokumen IN dan `departureWorkOrderNumber`/`departureSppNumber` dapat berbeda, dan keduanya harus dipertahankan. Histori movement, cycle selesai, durasi, koordinat, hash, serta replay tidak ditulis ulang.

BE/database menurunkan internal nullable `spp_departure_policy_version = 7` untuk setiap SPP OUT baru. Row lama tetap null tanpa backfill; ini bukan field API. Link Servis `resumed_from_service_transaction_id` yang benar tetap diturunkan/dilindungi, tetapi bukan syarat reuse. Korelasi yang tidak cocok tidak dibuat-buat.

Lock tetap unit → canonical SPP; returning IN dengan nomor berbeda mengunci kedua SPP secara deterministik. Lookup, locked submit, dan trigger INSERT memakai pemeriksaan histori lintas unit yang sama.

## Urutan rilis dan batas rollback

1. Operator melakukan backup, mencatat version/dirty state, dan menguji pada salinan disposable. Jangan gunakan DSN produksi untuk test.
2. Hentikan movement writers/semua replica dalam maintenance window; migration mengambil table lock.
3. Terapkan migration sampai **000007** sebelum BE baru. Jangan mengedit migration 000006 yang sudah deployed.
4. Deploy BE konsisten di semua replica, verifikasi health, lalu buka kembali writers. APP menghapus pembatasan lokal lama dan tetap mengikuti array BE.

Jenkins PROD hanya memublikasikan image. Operator menjalankan migration one-off Portainer dengan image rilis, entrypoint `/app/migrate`, argument `up`, lalu deploy server dengan entrypoint default. Lihat [panduan Portainer](production-portainer.md). Dokumen ini bukan bukti bahwa migration/deploy sudah dilakukan.

Downgrade 7→6 mengambil lock writer **sebelum** memeriksa audit dan **menolak jika ada satu saja SPP OUT policy 7**, termasuk Customer. Jika belum ada, trigger/index versi 6 dipulihkan tanpa menghapus histori/link Servis lama. Jangan menghapus audit, mematikan trigger, atau memaksa migration version agar downgrade lolos. Downgrade lebih lanjut dari 6 memiliki guard link Servis sendiri.

Rollback image pun harus ditinjau kompatibilitasnya: image lama masih membatasi reuse dan dapat gagal saat pola histori baru sudah ada. Jangan menganggap rollback image/schema setara atau otomatis aman. Jika migration gagal/dirty, hentikan rollout dan lakukan recovery yang disetujui tanpa membuang audit.

## Checklist nonproduksi

- Ulangi kedua tujuan beberapa kali setelah tiap kategori IN, termasuk setelah Kosongan dan ketika nomor IN/customer historis berbeda.
- Pastikan Customer tanpa kode customer dan SPP milik histori unit lain ditolak; unit/source salah, WO uniqueness, dan batas Bengkel/OTHER tetap berlaku.
- Pastikan OUT→OUT/IN→IN ditolak, cycle/durasi terpisah benar, snapshot departure asli utuh, dan retry mengembalikan transaksi lama.
- Uji upgrade dari versi 6 dengan link Servis lama, klaim SPP bersamaan antarunit, serta downgrade menunggu writer lalu menolak audit policy 7.
- Jangan membuat movement live tanpa persetujuan.

Referensi: [eligibility APP](mobile-spp-out-eligibility-handover.md), [returning IN](mobile-returning-document-handover.md), [operasional/desain](spp-service-continuation-operations.md).
