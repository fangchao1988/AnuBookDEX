// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/**
 * @title TestToken
 * @notice 测试网专用 ERC-20：任何人可 mint（水龙头），供 AnuBookDEX 联调真实代币结算。
 * @dev 仅用于测试网。maxSupply 上限防止被无限增发；decimals 可配置（
 *      AnuBookDEX 当前按「整数单位」下单，故部署为 0 位小数使 1 单位 = 1 代币）。
 */
contract TestToken is ERC20 {
    uint8 private immutable _customDecimals;
    uint256 public immutable maxSupply;

    constructor(
        string memory name_,
        string memory symbol_,
        uint8 decimals_,
        uint256 maxSupply_
    ) ERC20(name_, symbol_) {
        _customDecimals = decimals_;
        maxSupply = maxSupply_;
    }

    function decimals() public view override returns (uint8) {
        return _customDecimals;
    }

    /// @notice 公开铸币（测试网水龙头）。受 maxSupply 限制。
    function mint(address to, uint256 amount) external {
        require(amount > 0, "amount=0");
        require(totalSupply() + amount <= maxSupply, "max supply exceeded");
        _mint(to, amount);
    }
}
