# PROD: Publish Jenkins, Deploy Manual Lewat Portainer

## Batas Tanggung Jawab

- Jenkins dan DEV berada pada host yang sama. PROD berada pada server berbeda dan dikelola manual lewat Portainer.
- `TARGET_ENV=prod`: Jenkins menjalankan test/build, meminta approval, lalu push image bertag ke registry `172.16.17.19:5000`. **Tidak ada migration atau deployment container PROD dari Jenkins.** Pesan sukses berarti image dipublikasikan, bukan aplikasi sudah sehat.
- `TARGET_ENV=dev`: migration, deployment, health check, dan rollback DEV tetap berjalan seperti sebelumnya.
- Image yang sama berisi `/app/server`, `/app/migrate`, dan migration SQL; environment Jenkins tidak otomatis menjadi environment container Portainer.

## 1. Ambil Image Yang Tepat

Jalankan pipeline dengan `TARGET_ENV=prod`, periksa hasil quality gate, lalu approve publikasi. Salin **image lengkap** dari pesan `Production image published (not deployed)`; formatnya:

```text
172.16.17.19:5000/truckin-be:prod-<BUILD_NUMBER>-<COMMIT8>
```

Placeholder di atas bukan tag yang bisa langsung dipakai. Gunakan tag build aktual, bukan tag DEV/`latest`. Pilih environment/endpoint Portainer pada **server PROD**, pastikan server dapat pull registry, dan catat image/runtime settings lama untuk rollback. Jangan mengubah container DEV pada host Jenkins.

## 2. Isi Runtime Environment Consul

Untuk container migration maupun server, isi **Advanced container settings → Env** (nama menu dapat berbeda menurut versi Portainer):

| Nama | Nilai PROD |
| --- | --- |
| `CONSUL_HTTP_ADDR` | `http://172.16.17.17:8500` |
| `CONSUL_CONFIG_KEY` | `prod/be/truckin-be-config` |
| `CONSUL_ALLOW_INSECURE_HTTP` | `true` |

Alamat UI `http://172.16.17.17:8500/ui/dc1/kv/prod/be/truckin-be-config/edit` **bukan** nilai `CONSUL_HTTP_ADDR`. DEV memakai alamat dasar yang sama, tetapi key `dev/be/truckin-be-config`.

Jika Consul memakai ACL, inject `CONSUL_HTTP_TOKEN` melalui mekanisme secret/credential deployment yang disetujui. Jangan masukkan token ke Git, log, screenshot, atau chat. HTTP opt-in di atas mengikuti jaringan saat ini; HTTP mengirim konfigurasi/token tanpa TLS, sehingga HTTPS tetap lebih aman bila tersedia.

Periksa konfigurasi key PROD secara privat: pastikan database benar-benar database PROD, `server_port` sesuai mapping, dan semua nilai wajib runtime sudah tersedia. Jangan menyalin DSN/token DEV atau menampilkan isi JSON konfigurasi ke log. Server PROD harus dapat mengakses Consul dan dependency yang ditunjuk oleh konfigurasi tersebut; `localhost` di dalam container bukan host Consul/database.

## 3. Jalankan Migration Secara Manual Sebelum Server Baru

Setelah backup/database dan change window disetujui, buat container **one-off** terpisah di Portainer pada server PROD:

- Image: tag PROD aktual yang akan dipakai server.
- Environment: ketiga nilai Consul di atas dan ACL token bila diperlukan.
- Override **entrypoint** ke `/app/migrate`, command/arguments `up`; working directory tetap `/app`.
- Restart policy: **No**. Tidak perlu publish port; gunakan jaringan yang dapat mengakses Consul/database PROD.
- Jalankan satu job saja, tunggu selesai dan pastikan **exit code 0**. Database yang sudah pada versi terakhir juga dianggap sukses. Jangan deploy server baru jika migration gagal; jangan otomatis memakai `down` atau memaksa dirty-version untuk melewati error.

Mengubah command saja tanpa override entrypoint tidak cukup: entrypoint image default adalah `/app/server`. Migration ini menyentuh database PROD dan harus dijalankan operator secara sengaja, bukan sebagai bagian publish Jenkins.

## 4. Buat Ulang Container Server

Pada container aplikasi PROD, gunakan **Duplicate/Edit** atau edit konfigurasi stack sumber jika container dikelola oleh stack:

1. Pilih image PROD aktual; pertahankan network, volume, dan pengaturan deployment yang diperlukan.
2. Isi Env seperti bagian 2. Opsional: `GIN_MODE=release` untuk runtime PROD.
3. Gunakan entrypoint default `/app/server`, tanpa command `up`/override migration. Cocokkan port host dengan port aplikasi; default image dan health check adalah **8105**, jadi konfigurasi normal adalah `8105:8105` dengan `server_port=8105` di Consul.
4. Pilih restart policy operasional (misalnya `unless-stopped`), lalu **recreate/redeploy**. Restart container lama saja tidak menambahkan environment baru; bila memakai stack, simpan nilai di definisi stack agar redeploy berikutnya tidak kehilangan Env.

Setelah deploy, tunggu health status **healthy** dan periksa `/health` menghasilkan HTTP 200 pada endpoint PROD. Periksa log startup serta status sync/dependency; jangan membuat movement palsu untuk smoke test. Aplikasi membaca Consul pada startup, bukan live reload. Health endpoint sendiri tidak membuktikan semua API upstream bisnis sudah bisa diakses.

## Diagnosis Cepat Dan Rollback

| Pesan | Makna / tindakan |
| --- | --- |
| `Consul address and config key are required` | Salah satu env alamat/key belum masuk atau kosong; belum terjadi request ke Consul. Perbaiki Env pada container PROD lalu recreate. |
| `Consul address must be an HTTPS URL unless CONSUL_ALLOW_INSECURE_HTTP=true` | Alamat HTTP dipakai tanpa opt-in; sesuaikan Env atau gunakan HTTPS. |
| `read configuration from Consul` / status error | Cek akses jaringan, key, dan ACL secara privat; jangan menganggap ini masalah payload movement. |
| Config/database validation atau startup gagal | Periksa key PROD, migration, DSN, serta mandatory runtime fields secara privat. Jangan melewati validasi. |

Jika server baru gagal, hentikan rollout dan kembalikan image/runtime settings sebelumnya **hanya jika kompatibel dengan schema/config sekarang**. Jenkins tidak mengurus rollback PROD. Jangan downgrade schema secara otomatis atau mengubah histori movement. Untuk deployment PROD pertama, tidak ada image lama untuk rollback: hentikan kandidat yang gagal dan perbaiki konfigurasi sebelum mencoba lagi. Jangan membagikan dump environment/config yang berisi secret.
