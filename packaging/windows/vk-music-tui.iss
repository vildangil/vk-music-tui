; Inno Setup script for the Windows installer (vk-music-tui-setup-<arch>.exe).
; CI: iscc /DAppVersion=1.2.3 /DArch=amd64 /DSrcExe=path\to\vktui.exe packaging\windows\vk-music-tui.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef Arch
  #define Arch "amd64"
#endif
#ifndef SrcExe
  #define SrcExe "..\..\vktui.exe"
#endif
#ifndef IconFile
  #define IconFile ""
#endif

[Setup]
AppId={{8B1E8E2C-4C1A-4E7B-9C6B-7A1F2D7E5A11}
AppName=VK Music TUI
AppVerName=VK Music TUI {#AppVersion}
AppVersion={#AppVersion}
AppPublisher=vildangil
AppPublisherURL=https://github.com/vildangil/vk-music-tui
AppSupportURL=https://github.com/vildangil/vk-music-tui/issues
; per-user folder: no admin rights needed, and the player can keep music,
; the offline library and the cache right next to itself
DefaultDirName={localappdata}\Programs\vk-music-tui
DefaultGroupName=VK Music TUI
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputBaseFilename=vk-music-tui-setup-{#Arch}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes
UninstallDisplayIcon={app}\vktui.exe
UninstallDisplayName=VK Music TUI
CloseApplications=yes
#if IconFile != ""
SetupIconFile={#IconFile}
#endif
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#elif Arch == "386"
ArchitecturesAllowed=x86compatible
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif

[Languages]
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Ярлык на рабочем столе"; GroupDescription: "Ярлыки:"
Name: "mpv"; Description: "Установить mpv через winget (нужен для звука)"; GroupDescription: "Компоненты:"; Check: not MpvInstalled
Name: "path"; Description: "Добавить в PATH (запуск командой vktui из любого терминала)"; GroupDescription: "Компоненты:"

[Files]
Source: "{#SrcExe}"; DestDir: "{app}"; DestName: "vktui.exe"; Flags: ignoreversion
#if IconFile != ""
Source: "{#IconFile}"; DestDir: "{app}"; DestName: "vk-music-tui.ico"; Flags: ignoreversion
#endif

[Icons]
#if IconFile != ""
Name: "{group}\VK Music TUI"; Filename: "{app}\vktui.exe"; WorkingDir: "{app}"; IconFilename: "{app}\vk-music-tui.ico"
Name: "{autodesktop}\VK Music TUI"; Filename: "{app}\vktui.exe"; WorkingDir: "{app}"; IconFilename: "{app}\vk-music-tui.ico"; Tasks: desktopicon
#else
Name: "{group}\VK Music TUI"; Filename: "{app}\vktui.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\VK Music TUI"; Filename: "{app}\vktui.exe"; WorkingDir: "{app}"; Tasks: desktopicon
#endif
Name: "{group}\Добавить аккаунт VK"; Filename: "{app}\vktui.exe"; Parameters: "login"; WorkingDir: "{app}"
Name: "{group}\Папка с музыкой"; Filename: "{app}\music"
Name: "{group}\Удалить VK Music TUI"; Filename: "{uninstallexe}"

[Dirs]
Name: "{app}\music"; Flags: uninsneveruninstall

[Run]
Filename: "winget.exe"; Parameters: "install --id shinchiro.mpv -e --accept-source-agreements --accept-package-agreements"; StatusMsg: "Устанавливаю mpv..."; Tasks: mpv; Flags: waituntilterminated
Filename: "{app}\vktui.exe"; Description: "Запустить VK Music TUI"; Flags: postinstall nowait skipifsilent

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: path; Check: NeedsAddPath(ExpandConstant('{app}'))

[UninstallDelete]
; the cache is disposable; your downloaded music and offline library are kept
Type: filesandordirs; Name: "{app}\cache"

[Code]
function MpvInstalled(): Boolean;
begin
  Result := FileExists(ExpandConstant('{commonpf64}\MPV Player\mpv.exe'))
    or FileExists(ExpandConstant('{localappdata}\Microsoft\WinGet\Links\mpv.exe'))
    or FileExists(ExpandConstant('{%USERPROFILE}\scoop\shims\mpv.exe'));
end;

function NeedsAddPath(Dir: string): Boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;

procedure RemovePath(Dir: string);
var
  P: string;
  I: Integer;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', P) then exit;
  P := ';' + P + ';';
  I := Pos(';' + Uppercase(Dir) + ';', Uppercase(P));
  if I = 0 then exit;
  Delete(P, I, Length(Dir) + 1);
  P := Copy(P, 2, Length(P) - 2);
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', P);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemovePath(ExpandConstant('{app}'));
end;
