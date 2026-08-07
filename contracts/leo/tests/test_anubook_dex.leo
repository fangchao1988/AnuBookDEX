// AnuBookDEX Aleo 测试（Phase 1）
//
// 注：本程序所有 fn 都返回 Final（on-chain finalization），leo test 的 proof context
// 无法 .run() final 逻辑，因此这里只做最小编译/导入校验。
// 真正的不变量验证在 leo devnode 的 execute + mapping 查询（见 docs/Aleo改造方案.md §8）。
import anubook_dex.aleo;

program test_anubook_dex.aleo {

    @noupgrade
    constructor() {}

    @test
    fn test_harness_compiles() {
        // 仅校验测试程序可编译、import 链路正常
        assert_eq(1u8, 1u8);
    }
}
