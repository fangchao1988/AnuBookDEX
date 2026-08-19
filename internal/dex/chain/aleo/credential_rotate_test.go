package aleo

import "testing"

// TestExtractCredentialsRecord 回归：settle_vp 成功后 leo 输出 "➡️  Outputs" 下逐条打印
// 过渡输出的 record 明文，其中含 operator 的新 Credentials record（transfer_private_with_creds
// 输出，owner 保持 operator）。旋转凭证必须从该块解析出可复用的单行明文（与 config
// chain.aleo.operator-credentials 格式一致）。
func TestExtractCredentialsRecord(t *testing.T) {
	// 与 warn-2026081814.log 14:19 settle_vp 输出一致：第 4 条是新的 Credentials record。
	out := `➡️  Outputs

 • {
  owner: aleo1mnldqsfrwayzfep3t9zusu4y7yknsdhfwx76v5pajla35mrqn5xsvvurhy.private,
  amount: 3200u128.private,
  _nonce: 8124975079149927649895466315909440196110294559681810050642376905084192891262group.public,
  _version: 1u8.public
}
 • {
  owner: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal.private,
  amount: 0u128.private,
  _nonce: 7638302776943752644970849662573006836732578984389003219213884760421359921043group.public,
  _version: 1u8.public
}
 • {
  owner: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal.private,
  freeze_list_root: 3642222252059314292809609689035560016959342421640560347114299934615987159853field.private,
  _nonce: 5619024008784984711030672572527243591636444083342814568297933874087459274452group.public,
  _version: 1u8.public
}
`

	const operator = "aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal"
	creds, ok := extractCredentialsRecord(out, operator)
	if !ok {
		t.Fatal("extractCredentialsRecord: 应找到 operator 的新 Credentials record")
	}
	if got := credsNonce(creds); got != "5619024008784984711030672572527243591636444083342814568297933874087459274452" {
		t.Fatalf("nonce=%s want 5619...52", got)
	}
	if got := creds; got != "{ owner: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal.private, freeze_list_root: 3642222252059314292809609689035560016959342421640560347114299934615987159853field.private, _nonce: 5619024008784984711030672572527243591636444083342814568297933874087459274452group.public, _version: 1u8.public }" {
		t.Fatalf("creds=%s", got)
	}
}

// TestExtractCredentialsRecordOwnerMismatch 凭证输出 owner 非 operator（如误捕获用户凭证）时不得返回。
func TestExtractCredentialsRecordOwnerMismatch(t *testing.T) {
	out := ` • {
  owner: aleo1cwzattgxzw7kxklq0rp0dxntn27yf9qftjk2jrk5qv73r4lkngxqvhsw9v.private,
  freeze_list_root: 1111field.private,
  _nonce: 2222group.public,
  _version: 1u8.public
}
`
	if _, ok := extractCredentialsRecord(out, "aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal"); ok {
		t.Fatal("owner 不匹配 operator 时不应返回")
	}
}

// TestExtractCredentialsRecordNotConsumed 未消费凭证的 transition 输出无 Credentials record。
func TestExtractCredentialsRecordNotConsumed(t *testing.T) {
	out := `➡️  Outputs

 • {
  owner: aleo1cwzattgxzw7kxklq0rp0dxntn27yf9qftjk2jrk5qv73r4lkngxqvhsw9v.private,
  amount: 3200u128.private,
  _nonce: 7777group.public,
  _version: 1u8.public
}
`
	if _, ok := extractCredentialsRecord(out, "aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal"); ok {
		t.Fatal("无 Credentials record 时不应返回")
	}
}