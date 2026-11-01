{
  "targets": [{
    "target_name": "cardinex",
    "sources": ["src/addon.cc"],
    "include_dirs": ["<!(node -p \"require('node-addon-api').include_dir\")", "<(module_root_dir)/../../dist"],
    "defines": ["NAPI_CPP_EXCEPTIONS", "NAPI_VERSION=8"],
    "cflags_cc": ["-fexceptions"],
    "xcode_settings": { "GCC_ENABLE_CPP_EXCEPTIONS": "YES" },
    "msvs_settings": { "VCCLCompilerTool": { "ExceptionHandling": 1 } },
    "conditions": [
      ["OS=='win'", { "libraries": ["<(module_root_dir)/../../dist/cardinex.lib"] }],
      ["OS=='mac'", { "libraries": ["-L<(module_root_dir)/../../dist", "-lcardinex", "-Wl,-rpath,@loader_path"] }],
      ["OS=='linux'", { "libraries": ["-L<(module_root_dir)/../../dist", "-lcardinex", "-Wl,-rpath,'$$ORIGIN'"] }]
    ]
  }]
}
