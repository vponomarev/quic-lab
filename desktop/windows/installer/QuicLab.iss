#ifndef PayloadDir
  #error PayloadDir is required
#endif
#ifndef AppVersion
  #define AppVersion "0.1.0-beta.2"
#endif
#ifndef ProductName
  #define ProductName "QUIC Lab Windows Preview"
#endif
#ifndef ServiceName
  #define ServiceName "QuicLabWindowsPreview"
#endif
#ifndef InstallerId
  #define InstallerId "QUIC-Lab-Windows-E3CC2F30-76B7-4AC0-922B-391C7B53D160"
#endif

[Setup]
AppId={#InstallerId}
AppName=QUIC Lab Windows
AppVersion={#AppVersion}
AppPublisher=QUIC Lab
AppPublisherURL=https://github.com/vponomarev/quic-lab
DefaultDirName={autopf}\{#ProductName}
DisableDirPage=yes
DefaultGroupName=QUIC Lab
DisableProgramGroupPage=yes
OutputBaseFilename=quic-lab-windows-{#AppVersion}-x64-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64os
ArchitecturesInstallIn64BitMode=x64os
MinVersion=10.0.19041
PrivilegesRequired=admin
WizardStyle=modern
UninstallDisplayIcon={app}\ui\QuicLab.Windows.exe
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
DisableWelcomePage=no
InfoAfterFile=SetupInfo.txt

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Создать ярлык на рабочем столе"; Flags: unchecked

[Files]
Source: "Service.ps1"; Flags: dontcopy
Source: "Service.Common.ps1"; Flags: dontcopy
Source: "{#PayloadDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{commonprograms}\QUIC Lab\QUIC Lab"; Filename: "{app}\ui\QuicLab.Windows.exe"; WorkingDir: "{app}\ui"
Name: "{commondesktop}\QUIC Lab"; Filename: "{app}\ui\QuicLab.Windows.exe"; WorkingDir: "{app}\ui"; Tasks: desktopicon

[Run]
Filename: "{app}\ui\QuicLab.Windows.exe"; Description: "Запустить QUIC Lab и настроить подключение"; Flags: nowait postinstall skipifsilent runasoriginaluser

[Code]
var
  OwnerSID: String;
  Prepared, Installed: Boolean;

function Helper(Action, Script: String): Boolean;
var Code: Integer; Params: String;
begin
  Params := '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + Script +
    '" -Action ' + Action + ' -InstallDir "' + ExpandConstant('{app}') +
    '" -StateDir "' + ExpandConstant('{commonappdata}\{#ProductName}') +
    '" -ServiceName {#ServiceName}';
  if OwnerSID <> '' then Params := Params + ' -OwnerSID "' + OwnerSID + '"';
  if Action = 'Owner' then Params := Params + ' -OwnerFile "' + ExpandConstant('{tmp}\desktop-owner.txt') + '"';
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Params, '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var OwnerFile: String; I: Integer; Raw: AnsiString;
begin
  Result := '';
  ExtractTemporaryFile('Service.ps1');
  ExtractTemporaryFile('Service.Common.ps1');
  OwnerFile := ExpandConstant('{tmp}\desktop-owner.txt');
  if not Helper('Owner', ExpandConstant('{tmp}\Service.ps1')) then begin
    Result := 'Не удалось определить пользователя. Запустите установщик обычным двойным щелчком и подтвердите запрос Windows.'; Exit;
  end;
  if not LoadStringFromFile(OwnerFile, Raw) then begin Result := 'Не удалось прочитать пользователя установки.'; Exit; end;
  OwnerSID := Trim(String(Raw));
  if (Length(OwnerSID) < 12) or (Length(OwnerSID) > 190) then begin Result := 'Некорректный пользователь установки.'; Exit; end;
  for I := 1 to Length(OwnerSID) do
    if Pos(Copy(OwnerSID, I, 1), 'S-0123456789') = 0 then begin Result := 'Некорректный пользователь установки.'; Exit; end;
  Prepared := Helper('Prepare', ExpandConstant('{tmp}\Service.ps1'));
  if not Prepared then Result := 'Не удалось подготовить службу. Закройте QUIC Lab. Проверьте права администратора и существующую установку.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    Installed := Helper('Install', ExpandConstant('{app}\Service.ps1'));
    if not Installed then RaiseException('Файлы установлены, но служба не запущена. Настройки сохранены. Повторите установку; подробности в журнале установки.');
  end;
end;

procedure DeinitializeSetup;
begin
  if Prepared and not Installed then Helper('Resume', ExpandConstant('{tmp}\Service.ps1'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then begin
    OwnerSID := '';
    if not Helper('Remove', ExpandConstant('{app}\Service.ps1')) then
      RaiseException('Не удалось остановить и удалить службу. Удаление файлов отменено.');
  end;
end;
