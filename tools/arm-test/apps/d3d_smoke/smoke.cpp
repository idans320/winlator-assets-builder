// Minimal D3D11 / D3D9 smoke test for the ARM64 Wine harness.
//
//   smoke.exe             -> D3D11: device, swapchain, HLSL via d3dcompiler,
//                            one triangle, N presents
//   smoke.exe --d3d9      -> D3D9: device, fixed-function pipeline (no shaders),
//                            one triangle, N presents
//
// Why these two: DXVK 3.x replaced the shader compiler (dxbc-spirv) and
// reimplemented the D3D8/9 fixed-function pipeline as ubershaders, so a probe
// that only creates a device would not touch either path.
//
// Exit code 0 means every stage succeeded; each stage prints a line so a
// partial log still shows how far it got.

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <d3d11.h>
#include <d3d9.h>
#include <dxgi1_2.h>
#include <d3dcompiler.h>

#include <cstdio>
#include <cstdarg>
#include <cwchar>
#include <io.h>
#include <cstring>
#include <cstdlib>
#include <initializer_list>

// Winlator's on-screen log only carries Wine's debug output, so a guest
// program cannot rely on stdout/stderr reaching it. Everything the probe
// reports goes to a file next to the exe as well, which is the record we pull
// off the device.
static FILE *g_report = nullptr;

static void out(const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  vfprintf(stderr, fmt, ap);
  va_end(ap);
  if (g_report) {
    va_start(ap, fmt);
    vfprintf(g_report, fmt, ap);
    va_end(ap);
    fflush(g_report);
  }
}

#define CHECK(hr, what)                                                        \
  do {                                                                         \
    HRESULT hr_ = (hr);                                                        \
    if (FAILED(hr_)) {                                                         \
      out("FAIL %s: hr=0x%08lx\n", what, (unsigned long)hr_);                 \
      return 1;                                                                \
    }                                                                          \
    out("ok   %s\n", what);                                                    \
  } while (0)

static const int kWidth = 640;
static const int kHeight = 480;
static const int kFrames = 60;

static LRESULT CALLBACK wndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
  return DefWindowProcA(hwnd, msg, wp, lp);
}

static HWND makeWindow() {
  WNDCLASSA wc;
  std::memset(&wc, 0, sizeof(wc));
  wc.lpfnWndProc = wndProc;
  wc.hInstance = GetModuleHandleA(nullptr);
  wc.lpszClassName = "d3d_smoke";
  RegisterClassA(&wc);
  return CreateWindowExA(0, wc.lpszClassName, "d3d_smoke", WS_OVERLAPPEDWINDOW,
                         0, 0, kWidth, kHeight, nullptr, nullptr, wc.hInstance,
                         nullptr);
}

static const char *kVsSrc =
    "float4 main(uint id : SV_VertexID) : SV_Position {\n"
    "  float2 p = float2((id << 1) & 2, id & 2);\n"
    "  return float4(p * 2.0 - 1.0, 0.0, 1.0);\n"
    "}\n";

static const char *kPsSrc =
    "float4 main(float4 p : SV_Position) : SV_Target {\n"
    "  return float4(0.2, 0.6, 0.9, 1.0);\n"
    "}\n";

static ID3DBlob *compile(const char *src, const char *entry, const char *target) {
  ID3DBlob *code = nullptr;
  ID3DBlob *err = nullptr;
  HRESULT hr = D3DCompile(src, std::strlen(src), nullptr, nullptr, nullptr, entry,
                          target, 0, 0, &code, &err);
  if (FAILED(hr)) {
    out("FAIL D3DCompile %s: hr=0x%08lx %s\n", target,
                (unsigned long)hr, err ? (const char *)err->GetBufferPointer() : "");
    if (err)
      err->Release();
    if (code)
      code->Release();
    return nullptr;
  }
  if (err)
    err->Release();
  out("ok   D3DCompile %s\n", target);
  return code;
}

static int runD3D11(HWND hwnd) {
  ID3D11Device *dev = nullptr;
  ID3D11DeviceContext *ctx = nullptr;
  D3D_FEATURE_LEVEL fl = D3D_FEATURE_LEVEL_11_0;

  CHECK(D3D11CreateDevice(nullptr, D3D_DRIVER_TYPE_HARDWARE, nullptr, 0, nullptr,
                          0, D3D11_SDK_VERSION, &dev, &fl, &ctx),
        "D3D11CreateDevice(HARDWARE)");
  out("info feature level 0x%x\n", (unsigned)fl);

  IDXGIDevice *dxgiDev = nullptr;
  CHECK(dev->QueryInterface(__uuidof(IDXGIDevice), (void **)&dxgiDev),
        "QueryInterface(IDXGIDevice)");

  IDXGIAdapter *adapter = nullptr;
  CHECK(dxgiDev->GetAdapter(&adapter), "IDXGIDevice::GetAdapter");
  DXGI_ADAPTER_DESC ad;
  std::memset(&ad, 0, sizeof(ad));
  CHECK(adapter->GetDesc(&ad), "IDXGIAdapter::GetDesc");
  out("info adapter: %ls (vendor 0x%04x device 0x%04x)\n",
          ad.Description, ad.VendorId, ad.DeviceId);

  IDXGIFactory2 *factory = nullptr;
  CHECK(CreateDXGIFactory1(__uuidof(IDXGIFactory2), (void **)&factory),
        "CreateDXGIFactory1");

  DXGI_SWAP_CHAIN_DESC1 scd;
  std::memset(&scd, 0, sizeof(scd));
  scd.Width = kWidth;
  scd.Height = kHeight;
  scd.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
  scd.SampleDesc.Count = 1;
  scd.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
  scd.BufferCount = 2;
  scd.SwapEffect = DXGI_SWAP_EFFECT_FLIP_DISCARD;
  scd.AlphaMode = DXGI_ALPHA_MODE_IGNORE;

  IDXGISwapChain1 *swap = nullptr;
  CHECK(factory->CreateSwapChainForHwnd(dev, hwnd, &scd, nullptr, nullptr, &swap),
        "CreateSwapChainForHwnd(flip-discard)");

  ID3D11Texture2D *backbuffer = nullptr;
  CHECK(swap->GetBuffer(0, __uuidof(ID3D11Texture2D), (void **)&backbuffer),
        "IDXGISwapChain::GetBuffer");

  ID3D11RenderTargetView *rtv = nullptr;
  CHECK(dev->CreateRenderTargetView(backbuffer, nullptr, &rtv),
        "CreateRenderTargetView");

  ID3DBlob *vsBlob = compile(kVsSrc, "main", "vs_5_0");
  ID3DBlob *psBlob = compile(kPsSrc, "main", "ps_5_0");
  if (!vsBlob || !psBlob)
    return 1;

  ID3D11VertexShader *vs = nullptr;
  ID3D11PixelShader *ps = nullptr;
  CHECK(dev->CreateVertexShader(vsBlob->GetBufferPointer(), vsBlob->GetBufferSize(),
                                nullptr, &vs),
        "CreateVertexShader");
  CHECK(dev->CreatePixelShader(psBlob->GetBufferPointer(), psBlob->GetBufferSize(),
                               nullptr, &ps),
        "CreatePixelShader");

  const float clear[4] = {0.1f, 0.1f, 0.1f, 1.0f};
  ctx->OMSetRenderTargets(1, &rtv, nullptr);
  ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
  ctx->VSSetShader(vs, nullptr, 0);
  ctx->PSSetShader(ps, nullptr, 0);

  LARGE_INTEGER freq, t0, t1;
  QueryPerformanceFrequency(&freq);
  QueryPerformanceCounter(&t0);
  for (int i = 0; i < kFrames; i++) {
    ctx->ClearRenderTargetView(rtv, clear);
    ctx->Draw(3, 0);
    swap->Present(0, 0);
  }
  ctx->Flush();
  QueryPerformanceCounter(&t1);
  double ms = 1000.0 * double(t1.QuadPart - t0.QuadPart) / double(freq.QuadPart);
  out("info %d frames in %.1f ms (%.1f fps, includes first-use pipeline compile)\n",
              kFrames, ms, 1000.0 * kFrames / ms);

  ps->Release();
  vs->Release();
  psBlob->Release();
  vsBlob->Release();
  rtv->Release();
  backbuffer->Release();
  swap->Release();
  factory->Release();
  adapter->Release();
  dxgiDev->Release();
  ctx->Release();
  dev->Release();
  return 0;
}

static int runD3D9(HWND hwnd) {
  IDirect3D9 *d3d = Direct3DCreate9(D3D_SDK_VERSION);
  if (!d3d) {
    out("FAIL Direct3DCreate9\n");
    return 1;
  }
  out("ok   Direct3DCreate9\n");

  D3DPRESENT_PARAMETERS pp;
  std::memset(&pp, 0, sizeof(pp));
  pp.Windowed = TRUE;
  pp.SwapEffect = D3DSWAPEFFECT_DISCARD;
  pp.BackBufferFormat = D3DFMT_UNKNOWN;
  pp.BackBufferWidth = kWidth;
  pp.BackBufferHeight = kHeight;
  pp.hDeviceWindow = hwnd;
  pp.PresentationInterval = D3DPRESENT_INTERVAL_IMMEDIATE;

  IDirect3DDevice9 *dev = nullptr;
  CHECK(d3d->CreateDevice(D3DADAPTER_DEFAULT, D3DDEVTYPE_HAL, hwnd,
                          D3DCREATE_HARDWARE_VERTEXPROCESSING, &pp, &dev),
        "IDirect3D9::CreateDevice(HAL)");

  // Fixed-function pipeline: no shaders at all. DXVK 3.x renders this through
  // its new ubershader path.
  struct Vertex { float x, y, z, rhw; DWORD color; };
  const Vertex verts[3] = {
      {320.0f, 20.0f, 0.5f, 1.0f, 0xffff8000},
      { 20.0f, 460.0f, 0.5f, 1.0f, 0xff00ff00},
      {620.0f, 460.0f, 0.5f, 1.0f, 0xff0080ff},
  };

  CHECK(dev->SetFVF(D3DFVF_XYZRHW | D3DFVF_DIFFUSE), "SetFVF");

  LARGE_INTEGER freq, t0, t1;
  QueryPerformanceFrequency(&freq);
  QueryPerformanceCounter(&t0);
  for (int i = 0; i < kFrames; i++) {
    dev->Clear(0, nullptr, D3DCLEAR_TARGET, D3DCOLOR_XRGB(24, 24, 24), 1.0f, 0);
    dev->BeginScene();
    dev->DrawPrimitiveUP(D3DPT_TRIANGLELIST, 1, verts, sizeof(Vertex));
    dev->EndScene();
    dev->Present(nullptr, nullptr, nullptr, nullptr);
  }
  QueryPerformanceCounter(&t1);
  double ms = 1000.0 * double(t1.QuadPart - t0.QuadPart) / double(freq.QuadPart);
  out("info %d frames in %.1f ms (%.1f fps, fixed-function path)\n",
              kFrames, ms, 1000.0 * kFrames / ms);

  dev->Release();
  d3d->Release();
  return 0;
}

// ---------------------------------------------------------------------------
// Benchmark modes
//
// Two numbers matter when comparing DXVK 2.6.8 (async) against 3.1.1 (worker
// threads, blocking first use):
//
//   steady  - cost of the submit path once everything is cached. CPU-side
//             overhead of DXVK itself; no Present, so no compositor/vsync cap.
//   churn   - cost of the *first* draw with a pipeline state that has never
//             been seen. This is where async removes the stall by dropping the
//             draw for a frame instead of compiling it inline.
//
// Both report cpu_ms (time spent inside D3D calls) and sync_ms (until the GPU
// finishes, forced with an event query).
// ---------------------------------------------------------------------------

struct QpcTimer {
  LARGE_INTEGER freq, t0;
  QpcTimer() {
    QueryPerformanceFrequency(&freq);
    QueryPerformanceCounter(&t0);
  }
  double ms() const {
    LARGE_INTEGER t1;
    QueryPerformanceCounter(&t1);
    return 1000.0 * double(t1.QuadPart - t0.QuadPart) / double(freq.QuadPart);
  }
};

static void syncD3D11(ID3D11Device *dev, ID3D11DeviceContext *ctx) {
  D3D11_QUERY_DESC qd = {D3D11_QUERY_EVENT, 0};
  ID3D11Query *q = nullptr;
  if (FAILED(dev->CreateQuery(&qd, &q)))
    return;
  ctx->End(q);
  while (ctx->GetData(q, nullptr, 0, 0) == S_FALSE)
    Sleep(0);
  q->Release();
}

static void syncD3D9(IDirect3DDevice9 *dev) {
  IDirect3DQuery9 *q = nullptr;
  if (FAILED(dev->CreateQuery(D3DQUERYTYPE_EVENT, &q)))
    return;
  q->Issue(D3DISSUE_END);
  while (q->GetData(nullptr, 0, D3DGETDATA_FLUSH) == S_FALSE)
    Sleep(0);
  q->Release();
}

static int benchD3D11() {
  const int kSteadyFrames = 64;
  const int kDrawsPerFrame = 64;
  const int kChurnStates = 128;

  ID3D11Device *dev = nullptr;
  ID3D11DeviceContext *ctx = nullptr;
  D3D_FEATURE_LEVEL fl = D3D_FEATURE_LEVEL_11_0;
  CHECK(D3D11CreateDevice(nullptr, D3D_DRIVER_TYPE_HARDWARE, nullptr, 0, nullptr,
                          0, D3D11_SDK_VERSION, &dev, &fl, &ctx),
        "D3D11CreateDevice(HARDWARE)");

  IDXGIDevice *dxgiDev = nullptr;
  IDXGIAdapter *adapter = nullptr;
  DXGI_ADAPTER_DESC ad;
  std::memset(&ad, 0, sizeof(ad));
  if (SUCCEEDED(dev->QueryInterface(__uuidof(IDXGIDevice), (void **)&dxgiDev)) &&
      SUCCEEDED(dxgiDev->GetAdapter(&adapter)) &&
      SUCCEEDED(adapter->GetDesc(&ad)))
    out("info adapter: %ls\n", ad.Description);
  out("info feature level 0x%x\n", (unsigned)fl);

  // Offscreen target: no swapchain, no Present, nothing to throttle us.
  D3D11_TEXTURE2D_DESC td;
  std::memset(&td, 0, sizeof(td));
  td.Width = kWidth;
  td.Height = kHeight;
  td.MipLevels = 1;
  td.ArraySize = 1;
  td.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
  td.SampleDesc.Count = 1;
  td.Usage = D3D11_USAGE_DEFAULT;
  td.BindFlags = D3D11_BIND_RENDER_TARGET;
  ID3D11Texture2D *target = nullptr;
  CHECK(dev->CreateTexture2D(&td, nullptr, &target), "CreateTexture2D(offscreen)");

  ID3D11RenderTargetView *rtv = nullptr;
  CHECK(dev->CreateRenderTargetView(target, nullptr, &rtv), "CreateRenderTargetView");

  ID3DBlob *vsBlob = compile(kVsSrc, "main", "vs_5_0");
  ID3DBlob *psBlob = compile(kPsSrc, "main", "ps_5_0");
  if (!vsBlob || !psBlob)
    return 1;

  ID3D11VertexShader *vs = nullptr;
  ID3D11PixelShader *ps = nullptr;
  CHECK(dev->CreateVertexShader(vsBlob->GetBufferPointer(), vsBlob->GetBufferSize(),
                                nullptr, &vs),
        "CreateVertexShader");
  CHECK(dev->CreatePixelShader(psBlob->GetBufferPointer(), psBlob->GetBufferSize(),
                               nullptr, &ps),
        "CreatePixelShader");

  D3D11_VIEWPORT vp = {0.0f, 0.0f, float(kWidth), float(kHeight), 0.0f, 1.0f};
  ctx->RSSetViewports(1, &vp);
  ctx->OMSetRenderTargets(1, &rtv, nullptr);
  ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
  ctx->VSSetShader(vs, nullptr, 0);
  ctx->PSSetShader(ps, nullptr, 0);

  const float clear[4] = {0.1f, 0.1f, 0.1f, 1.0f};

  // Warm-up: first draws create the base pipeline and touch the swapchain-free
  // path once, so the steady phase measures the cached case.
  for (int i = 0; i < 4; i++) {
    ctx->ClearRenderTargetView(rtv, clear);
    ctx->Draw(3, 0);
  }
  syncD3D11(dev, ctx);

  QpcTimer steady;
  for (int f = 0; f < kSteadyFrames; f++) {
    ctx->ClearRenderTargetView(rtv, clear);
    for (int d = 0; d < kDrawsPerFrame; d++)
      ctx->Draw(3, 0);
  }
  double steadyCpu = steady.ms();
  QpcTimer steadySync;
  syncD3D11(dev, ctx);
  double steadySyncMs = steadySync.ms();

  int draws = kSteadyFrames * kDrawsPerFrame;
  out("BENCH d3d11 steady draws=%d cpu_ms=%.2f sync_ms=%.2f draws_per_s=%.0f\n",
              draws, steadyCpu, steadySyncMs, draws / (steadyCpu / 1000.0));

  // Churn: every blend state is a distinct DXVK pipeline variant, so this times
  // exactly the first-use compile that the async patches skip over.
  ID3D11BlendState *states[kChurnStates];
  std::memset(states, 0, sizeof(states));
  const D3D11_BLEND blends[8] = {
      D3D11_BLEND_ONE, D3D11_BLEND_ZERO, D3D11_BLEND_SRC_ALPHA,
      D3D11_BLEND_INV_SRC_ALPHA, D3D11_BLEND_DEST_COLOR,
      D3D11_BLEND_INV_DEST_COLOR, D3D11_BLEND_SRC_COLOR, D3D11_BLEND_INV_SRC_COLOR};
  for (int i = 0; i < kChurnStates; i++) {
    D3D11_BLEND_DESC bd;
    std::memset(&bd, 0, sizeof(bd));
    bd.RenderTarget[0].BlendEnable = TRUE;
    bd.RenderTarget[0].SrcBlend = blends[i % 8];
    bd.RenderTarget[0].DestBlend = blends[(i / 8) % 8];
    bd.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    bd.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    bd.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_ZERO;
    bd.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    bd.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL;
    if (FAILED(dev->CreateBlendState(&bd, &states[i])))
      return 1;
  }

  QpcTimer churn;
  for (int i = 0; i < kChurnStates; i++) {
    float factor[4] = {1.0f, 1.0f, 1.0f, 1.0f};
    ctx->OMSetBlendState(states[i], factor, 0xffffffff);
    ctx->Draw(3, 0);
  }
  double churnCpu = churn.ms();
  QpcTimer churnSync;
  syncD3D11(dev, ctx);
  double churnSyncMs = churnSync.ms();

  out("BENCH d3d11 churn pipelines=%d cpu_ms=%.2f sync_ms=%.2f ms_per_pipeline=%.2f\n",
              kChurnStates, churnCpu, churnSyncMs, churnCpu / kChurnStates);

  for (int i = 0; i < kChurnStates; i++)
    if (states[i])
      states[i]->Release();
  ps->Release();
  vs->Release();
  psBlob->Release();
  vsBlob->Release();
  rtv->Release();
  target->Release();
  if (adapter)
    adapter->Release();
  if (dxgiDev)
    dxgiDev->Release();
  ctx->Release();
  dev->Release();
  out("RESULT PASS\n");
  return 0;
}

static int benchD3D9(HWND hwnd, bool useVertexBuffer) {
  const int kSteadyFrames = 64;
  const int kDrawsPerFrame = 64;
  const int kChurnStates = 128;

  IDirect3D9 *d3d = Direct3DCreate9(D3D_SDK_VERSION);
  if (!d3d)
    return 1;

  D3DPRESENT_PARAMETERS pp;
  std::memset(&pp, 0, sizeof(pp));
  pp.Windowed = TRUE;
  pp.SwapEffect = D3DSWAPEFFECT_DISCARD;
  pp.BackBufferFormat = D3DFMT_UNKNOWN;
  pp.BackBufferWidth = kWidth;
  pp.BackBufferHeight = kHeight;
  pp.hDeviceWindow = hwnd;
  pp.PresentationInterval = D3DPRESENT_INTERVAL_IMMEDIATE;

  IDirect3DDevice9 *dev = nullptr;
  CHECK(d3d->CreateDevice(D3DADAPTER_DEFAULT, D3DDEVTYPE_HAL, hwnd,
                          D3DCREATE_HARDWARE_VERTEXPROCESSING, &pp, &dev),
        "CreateDevice(HAL)");

  struct Vertex { float x, y, z, rhw; DWORD color; };
  const Vertex verts[3] = {
      {320.0f, 20.0f, 0.5f, 1.0f, 0xffff8000},
      { 20.0f, 460.0f, 0.5f, 1.0f, 0xff00ff00},
      {620.0f, 460.0f, 0.5f, 1.0f, 0xff0080ff},
  };
  dev->SetFVF(D3DFVF_XYZRHW | D3DFVF_DIFFUSE);
  dev->SetRenderState(D3DRS_LIGHTING, FALSE);

  // DrawPrimitiveUP (user-pointer) and DrawPrimitive (vertex buffer) are
  // different paths inside DXVK's D3D9 backend: 3.x reworked user-pointer
  // buffer uploads, so the two can diverge even with identical draw counts.
  IDirect3DVertexBuffer9 *vb = nullptr;
  if (useVertexBuffer) {
    CHECK(dev->CreateVertexBuffer(sizeof(verts), 0, D3DFVF_XYZRHW | D3DFVF_DIFFUSE,
                                  D3DPOOL_MANAGED, &vb, nullptr),
          "CreateVertexBuffer");
    void *dst = nullptr;
    vb->Lock(0, sizeof(verts), &dst, 0);
    std::memcpy(dst, verts, sizeof(verts));
    vb->Unlock();
    dev->SetStreamSource(0, vb, 0, sizeof(Vertex));
  }

  out("info d3d9 draw path: %s\n", useVertexBuffer ? "vertex buffer" : "user pointer (UP)");

  auto frame = [&]() {
    dev->BeginScene();
    for (int d = 0; d < kDrawsPerFrame; d++) {
      if (useVertexBuffer)
        dev->DrawPrimitive(D3DPT_TRIANGLELIST, 0, 1);
      else
        dev->DrawPrimitiveUP(D3DPT_TRIANGLELIST, 1, verts, sizeof(Vertex));
    }
    dev->EndScene();
  };

  for (int i = 0; i < 4; i++)
    frame();
  syncD3D9(dev);

  QpcTimer steady;
  for (int f = 0; f < kSteadyFrames; f++)
    frame();
  double steadyCpu = steady.ms();
  QpcTimer steadySync;
  syncD3D9(dev);
  double steadySyncMs = steadySync.ms();

  int draws = kSteadyFrames * kDrawsPerFrame;
  out("BENCH d3d9 steady draws=%d cpu_ms=%.2f sync_ms=%.2f draws_per_s=%.0f\n",
              draws, steadyCpu, steadySyncMs, draws / (steadyCpu / 1000.0));

  // Render-state churn: D3D9 state changes feed the fixed-function pipeline,
  // which 3.x reimplements as ubershaders - a different code path from 2.x's
  // on-the-fly shader generation.
  QpcTimer churn;
  for (int i = 0; i < kChurnStates; i++) {
    dev->SetRenderState(D3DRS_ALPHABLENDENABLE, TRUE);
    dev->SetRenderState(D3DRS_SRCBLEND, D3DBLEND_ONE + (i % 8));
    dev->SetRenderState(D3DRS_DESTBLEND, D3DBLEND_ONE + ((i / 8) % 8));
    dev->SetRenderState(D3DRS_CULLMODE, D3DCULL_NONE + (i % 3));
    dev->SetRenderState(D3DRS_ZENABLE, (i % 2) ? TRUE : FALSE);
    dev->BeginScene();
    if (useVertexBuffer)
      dev->DrawPrimitive(D3DPT_TRIANGLELIST, 0, 1);
    else
      dev->DrawPrimitiveUP(D3DPT_TRIANGLELIST, 1, verts, sizeof(Vertex));
    dev->EndScene();
  }
  double churnCpu = churn.ms();
  QpcTimer churnSync;
  syncD3D9(dev);
  double churnSyncMs = churnSync.ms();

  out("BENCH d3d9 churn states=%d cpu_ms=%.2f sync_ms=%.2f ms_per_state=%.2f\n",
              kChurnStates, churnCpu, churnSyncMs, churnCpu / kChurnStates);

  if (vb)
    vb->Release();
  dev->Release();
  d3d->Release();
  out("RESULT PASS\n");
  return 0;
}

int main(int argc, char **argv) {
  // The report file lives beside the exe (C:\smoke\), so it can be pulled with
  // `adb shell run-as ... cat` after the container has closed.
  g_report = fopen("C:\\smoke\\result.txt", "w");
  setvbuf(stderr, nullptr, _IONBF, 0);
  const char *mode = argc > 1 ? argv[1] : "";
  bool d3d9 = std::strcmp(mode, "--d3d9") == 0 || std::strcmp(mode, "--bench9") == 0 ||
              std::strcmp(mode, "--bench9vb") == 0;
  bool bench = std::strcmp(mode, "--bench11") == 0 || std::strcmp(mode, "--bench9") == 0 ||
               std::strcmp(mode, "--bench9vb") == 0;
  bool useVB = std::strcmp(mode, "--bench9vb") == 0;
  out("info mode: %s\n", bench ? (useVB ? "bench9vb" : (d3d9 ? "bench9" : "bench11")) : (d3d9 ? "d3d9" : "d3d11"));

  HWND hwnd = makeWindow();
  if (!hwnd) {
    out("FAIL CreateWindowExA: %lu\n", (unsigned long)GetLastError());
    return 1;
  }
  out("ok   CreateWindowExA\n");

  // Which driver did the wrapper actually get told to load? The DXVK device
  // name only says "Wrapper", so read the plumbing directly.
  for (const char *key : {"ADRENOTOOLS_DRIVER_PATH", "ADRENOTOOLS_DRIVER_NAME",
                          "ADRENOTOOLS_HOOKS_PATH", "VK_ICD_FILENAMES",
                          "WRAPPER_VK_VERSION"}) {
    const char *val = getenv(key);
    out("info env %s=%s\n", key, val ? val : "(unset)");
  }

  int rc = bench ? (d3d9 ? benchD3D9(hwnd, useVB) : benchD3D11())
                 : (d3d9 ? runD3D9(hwnd) : runD3D11(hwnd));
  if (!bench)
    out("%s\n", rc == 0 ? "RESULT PASS" : "RESULT FAIL");
  return rc;
}

