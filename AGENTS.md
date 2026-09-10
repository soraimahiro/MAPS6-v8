# MAPS 專案開發規範與行為準則

本檔案定義 MAPS 系統的核心架構約束、硬體互動規範與行為防錯邊界。

---

## 1. 組態檔保護原則 (Configuration Preservation)
* **嚴禁私自變更出貨設定**：不得修改或重置使用者既有的 `configs/maps6.yaml` 設定值。
* **向後相容性設計**：若設定格式需擴充（例如支援整數秒數與時間字串），一律在 Go 程式碼層實作相容反序列化邏輯（UnmarshalYAML），嚴禁改動既有 YAML 檔案。
* **安裝防護**：安裝腳本更新時，若目標路徑已存在設定檔，必須予以保留，新預設值僅得存為 `.new`。

---

## 2. 嵌入式 Linux IPC 與通訊規範
* **Socket 存取權限**：守護程式（Daemon）建立之 Unix Domain Socket（如 `/var/run/maps6d.sock`）必須顯式設定 `chmod 0666`，允許非 root 一般使用者（如 `pi`）直接執行 CLI 工具。
* **長連線與自動重連**：IPC 伺服器端連線處理必須為持久監聽迴圈（Persistent Connection），不得在單一請求後立即 `Close()`；TUI 等長駐客戶端必須內建 Mutex 保護與斷線重連機制，防止後續查詢取得空值或 0.0。
* **安靜日誌準則 (Quiet Log Policy)**：定時感測器輪詢迴圈（高頻率運行）禁止輸出常態 INFO 日誌刷屏，僅在關鍵生命週期、狀態變更或通訊失敗（Error）時輸出日誌。

---

## 3. 硬體驅動與週邊互動 (Hardware & Driver)
* **OLED 點陣顯示**：
  * 優先使用內嵌點陣字型（如 `tinyfont/proggy`）直接在記憶體 Buffer 繪圖，避免對外部 TTF 字型檔案產生運行時依賴。
  * I2C 通訊優先採用 Linux 原生介面（`/dev/i2c-1` + ioctl `0x0703`），確保與樹莓派硬體通訊流程精準對齊。
* **Linux Input 鍵盤監聽**：
  * 不得假設 `/dev/input/event0` 即為鍵盤（通常為 HDMI CEC 或電源鍵）。
  * 必須動態解析 `/proc/bus/input/devices`（尋找 `Handlers=...kbd`）或掃描 `/dev/input/by-id/*kbd*`，並具備背景熱插拔（Hotplug）定時掃描。
  * 事件解析必須同時相容 32-bit (16 bytes) 與 64-bit (24 bytes) 的 `struct input_event`。
* **避免 UI 生命週期死鎖 (Deadlock Prevention)**：
  * 在 UI 事件回調中（持有內部鎖時），絕對不可同步呼叫外部模組生命週期切換（如 `registry.Disable` -> `mod.Stop()`）。
  * 模組的運作狀態應優先使用 `atomic.Bool` 等無鎖機制，避免回呼自身時發生自我死鎖（Self-Deadlock）。

---

## 4. 清單動態排序與資料呈現
* **確定性清單排序**：Go 語言 Map 迭代順序隨機，向 TUI 或外部回傳之模組/感測器清單，一律必須依字母順序（`sort.Strings`）穩定排序，防止介面不斷洗牌亂跳。
* **錯誤欄位純淨性**：狀態結構中的 `LastError` 欄位僅能存放真正發生的錯誤描述；正常的網路狀態（IP、SSID 等）應放置於獨立專用欄位，嚴禁污染 `LastError`。
